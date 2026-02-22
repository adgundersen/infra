package dns

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type Config struct {
	HostedZoneID string
	BaseDomain   string
}

type Client struct {
	r53 *route53.Client
	cfg Config
}

func NewClient(awsCfg aws.Config, cfg Config) *Client {
	return &Client{
		r53: route53.NewFromConfig(awsCfg),
		cfg: cfg,
	}
}

// CreateRecord points slug.crimata.com at the EC2 public IP.
func (c *Client) CreateRecord(ctx context.Context, slug, ip string) error {
	return c.changeRecord(ctx, slug, ip, r53types.ChangeActionCreate)
}

// DeleteRecord removes the Route53 record for a customer.
func (c *Client) DeleteRecord(ctx context.Context, slug, ip string) error {
	return c.changeRecord(ctx, slug, ip, r53types.ChangeActionDelete)
}

func (c *Client) changeRecord(ctx context.Context, slug, ip string, action r53types.ChangeAction) error {
	name := fmt.Sprintf("%s.%s", slug, c.cfg.BaseDomain)
	_, err := c.r53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(c.cfg.HostedZoneID),
		ChangeBatch: &r53types.ChangeBatch{
			Changes: []r53types.Change{
				{
					Action: action,
					ResourceRecordSet: &r53types.ResourceRecordSet{
						Name: aws.String(name),
						Type: r53types.RRTypeA,
						TTL:  aws.Int64(60),
						ResourceRecords: []r53types.ResourceRecord{
							{Value: aws.String(ip)},
						},
					},
				},
			},
		},
	})
	return err
}
