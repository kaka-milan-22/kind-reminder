package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crontab-reminder/internal/model"
)

func runWebhook(t *testing.T, ctx context.Context, cfg webhookConfig) model.StepResult {
	t.Helper()
	raw, _ := json.Marshal(cfg)
	return NewWebhookExecutor().Execute(ctx, &model.RunContext{}, model.Step{Config: raw})
}

func TestWebhookExecutorSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()

	res := runWebhook(t, context.Background(), webhookConfig{URL: srv.URL})
	if res.Status != "success" {
		t.Fatalf("status = %q (err %q), want success", res.Status, res.Error)
	}
	if !strings.Contains(res.Stdout, "HTTP 200") || !strings.Contains(res.Stdout, "pong") {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestWebhookExecutorHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := runWebhook(t, context.Background(), webhookConfig{URL: srv.URL})
	if res.Status != "failed" {
		t.Fatalf("status = %q, want failed for HTTP 500", res.Status)
	}
}

// TestWebhookExecutorHonorsContextDeadline confirms the request is bound by the
// step context, not a fixed client timeout.
func TestWebhookExecutorHonorsContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	res := runWebhook(t, ctx, webhookConfig{URL: srv.URL})
	if res.Status != "failed" {
		t.Fatalf("status = %q, want failed due to deadline", res.Status)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request took %v — context deadline not honored", elapsed)
	}
}
