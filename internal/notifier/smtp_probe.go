package notifier

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"slices"
	"strings"

	"crontab-reminder/internal/model"
)

var fakeIPNet19818 = mustParseCIDR("198.18.0.0/15")

type smtpResolution struct {
	Addresses       []string
	SuspectedFakeIP bool
	Hint            string
}

func ProbeSMTP(ctx context.Context, cfg EmailConfig, target string) model.SMTPDiagnostic {
	diag := model.SMTPDiagnostic{
		Host: cfg.Host,
		Port: cfg.Port,
		From: cfg.From,
		To:   target,
	}
	resolution := inspectSMTPResolution(ctx, cfg.Host)
	diag.ResolvedAddresses = resolution.Addresses
	diag.SuspectedFakeIP = resolution.SuspectedFakeIP
	diag.Hint = resolution.Hint

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return finishSMTPDiagnostic(diag, "tcp_connect", annotateSMTPError(err, resolution))
	}
	recordSMTPStage(&diag, "tcp_connect", true, "connected to "+addr, nil)

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return finishSMTPDiagnostic(diag, "smtp_greeting", annotateSMTPError(err, resolution))
	}
	defer func() {
		_ = client.Close()
	}()
	recordSMTPStage(&diag, "smtp_greeting", true, "server sent greeting", nil)

	if err := client.Hello("kind-reminder.local"); err != nil {
		return finishSMTPDiagnostic(diag, "ehlo", err)
	}
	recordSMTPStage(&diag, "ehlo", true, "EHLO accepted", nil)

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return finishSMTPDiagnostic(diag, "starttls", err)
		}
		recordSMTPStage(&diag, "starttls", true, "STARTTLS negotiated", nil)
	} else {
		recordSMTPStage(&diag, "starttls", true, "server does not advertise STARTTLS", nil)
	}

	if cfg.User != "" || cfg.Pass != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
			if err := client.Auth(auth); err != nil {
				return finishSMTPDiagnostic(diag, "auth", err)
			}
			recordSMTPStage(&diag, "auth", true, "SMTP AUTH succeeded", nil)
		} else {
			recordSMTPStage(&diag, "auth", true, "server does not advertise AUTH", nil)
		}
	} else {
		recordSMTPStage(&diag, "auth", true, "skipped because user/pass is empty", nil)
	}

	if cfg.From != "" {
		if err := client.Mail(cfg.From); err != nil {
			return finishSMTPDiagnostic(diag, "mail_from", err)
		}
		recordSMTPStage(&diag, "mail_from", true, "MAIL FROM accepted", nil)
	} else {
		recordSMTPStage(&diag, "mail_from", true, "skipped because from is empty", nil)
	}

	if target != "" {
		if err := client.Rcpt(target); err != nil {
			return finishSMTPDiagnostic(diag, "rcpt_to", err)
		}
		recordSMTPStage(&diag, "rcpt_to", true, "RCPT TO accepted", nil)
		if err := client.Reset(); err != nil && !errors.Is(err, net.ErrClosed) {
			recordSMTPStage(&diag, "reset", false, "", err)
		}
	} else {
		recordSMTPStage(&diag, "rcpt_to", true, "skipped because recipient is empty", nil)
	}

	if err := client.Quit(); err != nil && !errors.Is(err, net.ErrClosed) {
		return finishSMTPDiagnostic(diag, "quit", err)
	}
	recordSMTPStage(&diag, "quit", true, "SMTP session closed cleanly", nil)

	diag.Success = true
	if n := len(diag.Stages); n > 0 {
		diag.LastSuccessfulStage = diag.Stages[n-1].Name
	}
	return diag
}

func EmailConfigFromProvider(provider model.Provider) (EmailConfig, error) {
	var p struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		User string `json:"user"`
		Pass string `json:"pass"`
		From string `json:"from"`
	}
	if err := jsonUnmarshal(provider.Config, &p); err != nil {
		return EmailConfig{}, fmt.Errorf("email provider config: %w", err)
	}
	if p.Port == 0 {
		p.Port = 587
	}
	return EmailConfig{
		Host: p.Host,
		Port: p.Port,
		User: p.User,
		Pass: p.Pass,
		From: p.From,
	}, nil
}

func inspectSMTPResolution(ctx context.Context, host string) smtpResolution {
	var res smtpResolution
	host = strings.TrimSpace(host)
	if host == "" {
		return res
	}
	if ip := net.ParseIP(host); ip != nil {
		addr := ip.String()
		res.Addresses = []string{addr}
		res.SuspectedFakeIP, res.Hint = classifySMTPAddress(ip)
		return res
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		res.Hint = "DNS lookup failed: " + err.Error()
		return res
	}
	seen := make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		ip := addr.IP
		if ip == nil {
			continue
		}
		s := ip.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		res.Addresses = append(res.Addresses, s)
		if fake, hint := classifySMTPAddress(ip); fake {
			res.SuspectedFakeIP = true
			if res.Hint == "" {
				res.Hint = hint
			}
		}
	}
	slices.Sort(res.Addresses)
	return res
}

func annotateSMTPError(err error, resolution smtpResolution) error {
	if err == nil || resolution.Hint == "" {
		return err
	}
	return fmt.Errorf("%w (%s)", err, resolution.Hint)
}

func recordSMTPStage(diag *model.SMTPDiagnostic, name string, ok bool, detail string, err error) {
	stage := model.SMTPDiagnosticStage{Name: name, OK: ok, Detail: detail}
	if err != nil {
		stage.Error = err.Error()
	}
	diag.Stages = append(diag.Stages, stage)
	if ok {
		diag.LastSuccessfulStage = name
	}
}

func finishSMTPDiagnostic(diag model.SMTPDiagnostic, stage string, err error) model.SMTPDiagnostic {
	recordSMTPStage(&diag, stage, false, "", err)
	diag.FailedStage = stage
	if err != nil {
		diag.Error = err.Error()
	}
	return diag
}

func classifySMTPAddress(ip net.IP) (bool, string) {
	if ip == nil {
		return false, ""
	}
	if fakeIPNet19818.Contains(ip) {
		return true, fmt.Sprintf("resolved to %s in 198.18.0.0/15, a fake-IP/test range often introduced by proxy tools such as Shadowrocket in fake-ip mode", ip.String())
	}
	return false, ""
}

func mustParseCIDR(raw string) *net.IPNet {
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		panic(err)
	}
	return network
}

func jsonUnmarshal(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}
