package notify

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
)

type Config struct {
	FromEmail  string
	BaseDomain string
}

type Client struct {
	ses *ses.Client
	cfg Config
}

func NewClient(awsCfg aws.Config, cfg Config) *Client {
	return &Client{
		ses: ses.NewFromConfig(awsCfg),
		cfg: cfg,
	}
}

func (c *Client) SendWelcome(ctx context.Context, email, slug, password string) error {
	url := fmt.Sprintf("https://%s.%s", slug, c.cfg.BaseDomain)
	subject := "Your Crimata hub is ready"
	text := fmt.Sprintf("Your hub is live at %s\n\nPassword: %s\n\nChange your password in Settings after logging in.\n\n— Crimata", url, password)
	html := fmt.Sprintf(`<p>Your hub is live at <a href="%s">%s</a></p><p><strong>Password:</strong> <code>%s</code></p><p style="color:#555">Change your password in Settings after logging in.</p><p>— Crimata</p>`, url, url, password)

	_, err := c.ses.SendEmail(ctx, &ses.SendEmailInput{
		Source:      aws.String(c.cfg.FromEmail),
		Destination: &sestypes.Destination{ToAddresses: []string{email}},
		Message: &sestypes.Message{
			Subject: &sestypes.Content{Data: aws.String(subject)},
			Body: &sestypes.Body{
				Text: &sestypes.Content{Data: aws.String(text)},
				Html: &sestypes.Content{Data: aws.String(html)},
			},
		},
	})
	return err
}

func (c *Client) SendDataExport(ctx context.Context, email, downloadURL string) error {
	subject := "Your Crimata data export is ready"
	text := fmt.Sprintf("Your data export is ready for download:\n\n%s\n\nThis link expires in 24 hours.\n\n— Crimata", downloadURL)
	html := fmt.Sprintf(`<p>Your data export is ready: <a href="%s">Download</a></p><p style="color:#555">This link expires in 24 hours.</p><p>— Crimata</p>`, downloadURL)

	_, err := c.ses.SendEmail(ctx, &ses.SendEmailInput{
		Source:      aws.String(c.cfg.FromEmail),
		Destination: &sestypes.Destination{ToAddresses: []string{email}},
		Message: &sestypes.Message{
			Subject: &sestypes.Content{Data: aws.String(subject)},
			Body: &sestypes.Body{
				Text: &sestypes.Content{Data: aws.String(text)},
				Html: &sestypes.Content{Data: aws.String(html)},
			},
		},
	})
	return err
}
