package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crontab-reminder/internal/model"
)

type WebhookNotifier struct {
	baseURL string
	client  *http.Client
}

func NewWebhookNotifier(baseURL string, timeoutSeconds int) *WebhookNotifier {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}
	return &WebhookNotifier{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
	}
}

func (n *WebhookNotifier) Send(ctx context.Context, payload model.NotificationPayload, target string) error {
	url, err := n.resolveTarget(target)
	if err != nil {
		return err
	}

	body := map[string]string{
		"title":        payload.Title,
		"message":      payload.Message,
		"job_id":       payload.JobID,
		"execution_id": payload.ExecutionID,
		"scheduled_at": payload.ScheduledAt.UTC().Format(time.RFC3339),
		"triggered_at": payload.TriggeredAt.UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status: %d", resp.StatusCode)
	}
	return nil
}

func (n *WebhookNotifier) resolveTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("webhook target is required")
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target, nil
	}
	if n.baseURL == "" {
		return "", fmt.Errorf("webhook base_url is required for non-absolute target")
	}
	if strings.HasPrefix(target, "/") {
		return n.baseURL + target, nil
	}
	return n.baseURL + "/" + target, nil
}
