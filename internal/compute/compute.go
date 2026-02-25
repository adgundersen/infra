package compute

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"golang.org/x/crypto/ssh"
)

//go:embed provision.sh
var provisionScript []byte

type Config struct {
	AMI             string
	InstanceType    string
	SecurityGroupID string
	SubnetID        string
}

type Instance struct {
	InstanceID    string
	PublicIP      string
	SSHPrivateKey string // PEM encoded
}

type Client struct {
	ec2 *ec2.Client
	cfg Config
}

func NewClient(awsCfg aws.Config, cfg Config) *Client {
	return &Client{
		ec2: ec2.NewFromConfig(awsCfg),
		cfg: cfg,
	}
}

// Launch starts a new EC2 instance, injecting a generated SSH public key.
func (c *Client) Launch(ctx context.Context, slug string) (*Instance, error) {
	privateKey, publicKey, err := generateSSHKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate ssh key: %w", err)
	}

	userData := fmt.Sprintf("#!/bin/bash\nmkdir -p /root/.ssh\necho '%s' >> /root/.ssh/authorized_keys\nchmod 600 /root/.ssh/authorized_keys\n", publicKey)

	out, err := c.ec2.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:          aws.String(c.cfg.AMI),
		InstanceType:     ec2types.InstanceType(c.cfg.InstanceType),
		MinCount:         aws.Int32(1),
		MaxCount:         aws.Int32(1),
		SecurityGroupIds: []string{c.cfg.SecurityGroupID},
		SubnetId:         aws.String(c.cfg.SubnetID),
		UserData:         aws.String(base64.StdEncoding.EncodeToString([]byte(userData))),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("crimata-" + slug)},
					{Key: aws.String("crimata:slug"), Value: aws.String(slug)},
					{Key: aws.String("crimata:managed"), Value: aws.String("true")},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("run instances: %w", err)
	}

	instance := out.Instances[0]
	return &Instance{
		InstanceID:    aws.ToString(instance.InstanceId),
		PublicIP:      aws.ToString(instance.PublicIpAddress),
		SSHPrivateKey: privateKey,
	}, nil
}

// WaitUntilReady waits for the instance to be running and SSH-reachable.
func (c *Client) WaitUntilReady(ctx context.Context, instanceID, publicIP string) error {
	// Wait for EC2 running state
	waiter := ec2.NewInstanceRunningWaiter(c.ec2)
	if err := waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}, 5*time.Minute); err != nil {
		return err
	}

	// Poll until SSH port is open
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", publicIP+":22", 5*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for SSH on %s", publicIP)
}

// Provision runs provision.sh on the instance over SSH.
func (c *Client) Provision(ctx context.Context, publicIP, privateKeyPEM, slug, password, dbPassword string) error {
	signer, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            "ubuntu",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// Retry SSH connection — user data script may still be running
	var client *ssh.Client
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		client, err = ssh.Dial("tcp", publicIP+":22", config)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	if client == nil {
		return fmt.Errorf("could not connect via SSH: %w", err)
	}
	defer client.Close()

	// Upload and run provision.sh
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new ssh session: %w", err)
	}
	defer session.Close()

	session.Stdin = bytes.NewReader(provisionScript)
	cmd := fmt.Sprintf("bash -s -- %s %s %s", slug, password, dbPassword)
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("provision script: %w", err)
	}
	return nil
}

// Terminate shuts down a customer's EC2 instance.
func (c *Client) Terminate(ctx context.Context, instanceID string) error {
	_, err := c.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	return err
}

func generateSSHKeyPair() (privateKeyPEM string, authorizedKey string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", err
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}

	return string(privPEM), string(ssh.MarshalAuthorizedKey(pub)), nil
}

func parsePrivateKey(pemBytes string) (ssh.Signer, error) {
	return ssh.ParsePrivateKey([]byte(pemBytes))
}
