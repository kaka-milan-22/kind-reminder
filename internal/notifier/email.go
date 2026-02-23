package notifier

import (
	"context"
	"fmt"
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
		return err
	}
	_ = conn.Close()

	if err := smtp.SendMail(addr, auth, n.cfg.From, []string{target}, []byte(msg.String())); err != nil {
		return err
	}
	return nil
}
