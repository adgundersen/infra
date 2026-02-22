package compute

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type Config struct {
	AMI             string // Ubuntu 24.04 LTS
	InstanceType    string // t3.micro
	SecurityGroupID string
	SubnetID        string
	IAMInstanceProfile string // must have SSM permissions
	KeyName         string
}

type Instance struct {
	InstanceID string
	PublicIP   string
}

type Client struct {
	ec2 *ec2.Client
	ssm *ssm.Client
	cfg Config
}

func NewClient(awsCfg aws.Config, cfg Config) *Client {
	return &Client{
		ec2: ec2.NewFromConfig(awsCfg),
		ssm: ssm.NewFromConfig(awsCfg),
		cfg: cfg,
	}
}

// Launch starts a new EC2 instance for a customer and returns its details.
func (c *Client) Launch(ctx context.Context, slug string) (*Instance, error) {
	out, err := c.ec2.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:      aws.String(c.cfg.AMI),
		InstanceType: ec2types.InstanceType(c.cfg.InstanceType),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		IamInstanceProfile: &ec2types.IamInstanceProfileSpecification{
			Name: aws.String(c.cfg.IAMInstanceProfile),
		},
		SecurityGroupIds: []string{c.cfg.SecurityGroupID},
		SubnetId:         aws.String(c.cfg.SubnetID),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"),           Value: aws.String("crimata-" + slug)},
					{Key: aws.String("crimata:slug"),   Value: aws.String(slug)},
					{Key: aws.String("crimata:managed"), Value: aws.String("true")},
				},
			},
		},
		UserData: aws.String(bootstrapScript()),
	})
	if err != nil {
		return nil, fmt.Errorf("run instances: %w", err)
	}

	instance := out.Instances[0]
	return &Instance{
		InstanceID: aws.ToString(instance.InstanceId),
		PublicIP:   aws.ToString(instance.PublicIpAddress),
	}, nil
}

// WaitUntilReady waits for the instance to be running and SSM-reachable.
func (c *Client) WaitUntilReady(ctx context.Context, instanceID string) error {
	waiter := ec2.NewInstanceRunningWaiter(c.ec2)
	return waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}, 5*time.Minute)
}

// Provision runs the crimata provisioning script on the instance via SSM.
func (c *Client) Provision(ctx context.Context, instanceID, slug, password, dbPassword string) error {
	script := fmt.Sprintf(`
		curl -fsSL https://raw.githubusercontent.com/adgundersen/crimata/main/scripts/provision.sh \
		  | bash -s -- %s %s %s
	`, slug, password, dbPassword)

	out, err := c.ssm.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:  []string{instanceID},
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters: map[string][]string{
			"commands": {script},
		},
	})
	if err != nil {
		return fmt.Errorf("send command: %w", err)
	}

	// Wait for command to complete
	commandID := aws.ToString(out.Command.CommandId)
	return c.waitForCommand(ctx, instanceID, commandID)
}

// Terminate shuts down a customer's EC2 instance.
func (c *Client) Terminate(ctx context.Context, instanceID string) error {
	_, err := c.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	return err
}

func (c *Client) waitForCommand(ctx context.Context, instanceID, commandID string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			out, err := c.ssm.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
				CommandId:  aws.String(commandID),
				InstanceId: aws.String(instanceID),
			})
			if err != nil {
				continue
			}
			switch out.Status {
			case "Success":
				return nil
			case "Failed", "Cancelled", "TimedOut":
				return fmt.Errorf("provisioning script failed: %s", aws.ToString(out.StandardErrorContent))
			}
		}
	}
}

// bootstrapScript installs the SSM agent on first boot so we can run commands.
func bootstrapScript() string {
	return `#!/bin/bash
		snap install amazon-ssm-agent --classic
		systemctl enable snap.amazon-ssm-agent.amazon-ssm-agent.service
		systemctl start snap.amazon-ssm-agent.amazon-ssm-agent.service
	`
}
