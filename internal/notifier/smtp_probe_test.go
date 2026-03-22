package notifier

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestClassifySMTPAddressFlagsFakeIPRange(t *testing.T) {
	ip := net.ParseIP("198.18.0.23")
	fake, hint := classifySMTPAddress(ip)
	if !fake {
		t.Fatal("expected fake-ip detection for 198.18.0.23")
	}
	if !strings.Contains(hint, "Shadowrocket") {
		t.Fatalf("expected Shadowrocket hint, got %q", hint)
	}
}

func TestProbeSMTPReportsGreetingFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	diag := ProbeSMTP(ctx, EmailConfig{
		Host: "127.0.0.1",
		Port: ln.Addr().(*net.TCPAddr).Port,
	}, "")

	if diag.Success {
		t.Fatal("expected diagnostic failure")
	}
	if diag.FailedStage != "smtp_greeting" {
		t.Fatalf("expected smtp_greeting failure, got %q", diag.FailedStage)
	}
	if len(diag.Stages) < 2 {
		t.Fatalf("expected at least 2 stages, got %#v", diag.Stages)
	}
	if !diag.Stages[0].OK || diag.Stages[0].Name != "tcp_connect" {
		t.Fatalf("expected successful tcp_connect stage, got %#v", diag.Stages[0])
	}
	if diag.Stages[1].Name != "smtp_greeting" || diag.Stages[1].OK {
		t.Fatalf("expected failed smtp_greeting stage, got %#v", diag.Stages[1])
	}
}
