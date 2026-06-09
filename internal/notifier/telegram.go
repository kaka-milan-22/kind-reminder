package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"crontab-reminder/internal/model"
)

// telegramMaxChars is Telegram's hard limit for a sendMessage `text` field
// (4096 UTF-8 code points). Longer text is rejected with HTTP 400
// "message is too long", which would fail an otherwise-successful step.
const telegramMaxChars = 4096

// truncateForTelegram caps text at telegramMaxChars runes, appending a marker
// when it had to cut. It counts runes (not bytes) so it never splits a
// multibyte character into invalid UTF-8.
func truncateForTelegram(text string) string {
	r := []rune(text)
	if len(r) <= telegramMaxChars {
		return text
	}
	const marker = "\n…[truncated]"
	keep := telegramMaxChars - len([]rune(marker))
	return string(r[:keep]) + marker
}

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
		"text":    truncateForTelegram(FormatNotification(payload)),
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if desc := strings.TrimSpace(string(body)); desc != "" {
			return fmt.Errorf("telegram status: %d: %s", resp.StatusCode, desc)
		}
		return fmt.Errorf("telegram status: %d", resp.StatusCode)
	}
	return nil
}
