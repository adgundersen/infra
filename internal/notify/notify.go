package notify

import (
	"context"
	"fmt"

	resend "github.com/resend/resend-go/v2"
)

type Config struct {
	ApiKey     string
	FromEmail  string
	BaseDomain string
}

type Client struct {
	resend *resend.Client
	cfg    Config
}

func NewClient(cfg Config) *Client {
	return &Client{
		resend: resend.NewClient(cfg.ApiKey),
		cfg:    cfg,
	}
}

func (c *Client) SendWelcome(_ context.Context, email, slug, password string) error {
	url := fmt.Sprintf("https://%s.%s", slug, c.cfg.BaseDomain)

	_, err := c.resend.Emails.Send(&resend.SendEmailRequest{
		From:    c.cfg.FromEmail,
		To:      []string{email},
		Subject: "Your Crimata hub is ready",
		Text:    fmt.Sprintf("Your hub is live at %s\n\nUsername: %s\nPassword: %s\n\nChange your password in Settings after logging in.\n\n— Crimata", url, slug, password),
		Html:    fmt.Sprintf(`<p>Your hub is live at <a href="%s">%s</a></p><p><strong>Username:</strong> <code>%s</code><br><strong>Password:</strong> <code>%s</code></p><p style="color:#555">Change your password in Settings after logging in.</p><p>— Crimata</p>`, url, url, slug, password),
	})
	return err
}

func (c *Client) SendDataExport(_ context.Context, email, downloadURL string) error {
	_, err := c.resend.Emails.Send(&resend.SendEmailRequest{
		From:    c.cfg.FromEmail,
		To:      []string{email},
		Subject: "Your Crimata data export is ready",
		Text:    fmt.Sprintf("Your data export is ready for download:\n\n%s\n\nThis link expires in 24 hours.\n\n— Crimata", downloadURL),
		Html:    fmt.Sprintf(`<p>Your data export is ready: <a href="%s">Download</a></p><p style="color:#555">This link expires in 24 hours.</p><p>— Crimata</p>`, downloadURL),
	})
	return err
}
