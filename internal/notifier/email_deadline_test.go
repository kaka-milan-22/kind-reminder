package notifier

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"crontab-reminder/internal/model"
)

// TestEmailSendHonorsDeadline verifies that a server which accepts the TCP
// connection but never speaks SMTP cannot hang Send past the context deadline.
// Before the conn.SetDeadline fix this blocked until the server closed (~3s),
// because net/smtp's blocking reads ignore context.
func TestEmailSendHonorsDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		time.Sleep(3 * time.Second) // accept, then stall: never send greeting
		_ = conn.Close()
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	n := NewEmailNotifier(EmailConfig{Host: host, Port: port, From: "a@example.com"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = n.Send(ctx, model.NotificationPayload{Title: "t"}, "b@example.com")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from stalling server")
	}
	if elapsed > time.Second {
		t.Fatalf("Send blocked %v — connection deadline not honored", elapsed)
	}
}
