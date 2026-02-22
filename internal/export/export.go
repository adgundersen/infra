package export

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type Config struct {
	S3Bucket string
	Region   string
}

type Client struct {
	s3  *s3.Client
	ssm *ssm.Client
	cfg Config
}

func NewClient(awsCfg aws.Config, cfg Config) *Client {
	return &Client{
		s3:  s3.NewFromConfig(awsCfg),
		ssm: ssm.NewFromConfig(awsCfg),
		cfg: cfg,
	}
}

// Export dumps the customer's Postgres DB and files, uploads to S3,
// and returns a pre-signed download URL valid for 24 hours.
func (c *Client) Export(ctx context.Context, instanceID, slug string) (string, error) {
	key := fmt.Sprintf("exports/%s-%d.tar.gz", slug, time.Now().Unix())

	// Run export script on the EC2 via SSM
	script := fmt.Sprintf(`
		pg_dump crimata > /tmp/crimata.sql
		tar -czf /tmp/export.tar.gz /tmp/crimata.sql /opt/crimata/data
		aws s3 cp /tmp/export.tar.gz s3://%s/%s
	`, c.cfg.S3Bucket, key)

	_, err := c.ssm.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:  []string{instanceID},
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters:   map[string][]string{"commands": {script}},
	})
	if err != nil {
		return "", fmt.Errorf("export script: %w", err)
	}

	// Generate pre-signed URL valid for 24 hours
	presigner := s3.NewPresignClient(c.s3)
	req, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(24*time.Hour))
	if err != nil {
		return "", fmt.Errorf("presign: %w", err)
	}

	return req.URL, nil
}
