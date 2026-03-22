package notifier

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"

	"crontab-reminder/internal/model"
)

type EmailConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

type EmailNotifier struct {
	cfg EmailConfig
}

func NewEmailNotifier(cfg EmailConfig) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

func (n *EmailNotifier) Send(ctx context.Context, payload model.NotificationPayload, target string) error {
	_ = ctx
	addr := fmt.Sprintf("%s:%d", n.cfg.Host, n.cfg.Port)
	auth := smtp.PlainAuth("", n.cfg.User, n.cfg.Pass, n.cfg.Host)

	msg := strings.Builder{}
	msg.WriteString(fmt.Sprintf("From: %s\r\n", n.cfg.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", target))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", payload.Title))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(FormatNotification(payload))

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp connect %s: %w", addr, err)
	}
	client, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp greeting %s: %w", addr, err)
	}
	defer func() {
		_ = client.Close()
	}()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: n.cfg.Host}); err != nil {
			return fmt.Errorf("smtp STARTTLS %s: %w", addr, err)
		}
	}

	if ok, _ := client.Extension("AUTH"); ok {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp AUTH %s as %q: %w", addr, n.cfg.User, err)
		}
	}

	if err := client.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("smtp MAIL FROM %q via %s: %w", n.cfg.From, addr, err)
	}
	if err := client.Rcpt(target); err != nil {
		return fmt.Errorf("smtp RCPT TO %q via %s: %w", target, addr, err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA start via %s: %w", addr, err)
	}
	if _, err := io.WriteString(wc, msg.String()); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtp DATA write via %s: %w", addr, err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp DATA finish via %s: %w", addr, err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp QUIT via %s: %w", addr, err)
	}
	return nil
}
