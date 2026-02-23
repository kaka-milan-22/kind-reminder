package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"crontab-reminder/internal/model"
)

type TelegramNotifier struct {
	token  string
	client *http.Client
}

func NewTelegramNotifier(token string) *TelegramNotifier {
	return &TelegramNotifier{
		token: token,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (n *TelegramNotifier) Send(ctx context.Context, payload model.NotificationPayload, target string) error {
	reqPayload := map[string]any{
		"chat_id": target,
		"text":    FormatNotification(payload),
	}
	b, _ := json.Marshal(reqPayload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.token)
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
		return fmt.Errorf("telegram status: %d", resp.StatusCode)
	}
	return nil
}
