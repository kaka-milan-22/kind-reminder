package notifier

import (
	"context"
	"encoding/json"
	"fmt"

	"crontab-reminder/internal/model"
)

// SendViaChannelResource dispatches a notification through a named channel.
// provider is the bound Provider (may be nil); if nil, fallback registry is used.
func SendViaChannelResource(ctx context.Context, payload model.NotificationPayload, ch model.ChannelResource, provider *model.Provider, fallback Registry) error {
	switch ch.Type {
	case model.ChannelTelegram:
		return sendTelegram(ctx, payload, ch.Config, provider, fallback)
	case model.ChannelEmail:
		return sendEmail(ctx, payload, ch.Config, provider, fallback)
	case model.ChannelWebhook:
		return sendWebhook(ctx, payload, ch.Config, provider, fallback)
	default:
		return fmt.Errorf("unknown channel type: %s", ch.Type)
	}
}

func sendTelegram(ctx context.Context, payload model.NotificationPayload, chCfg json.RawMessage, provider *model.Provider, fallback Registry) error {
	var c struct {
		ChatID string `json:"chat_id"`
	}
	if err := json.Unmarshal(chCfg, &c); err != nil {
		return fmt.Errorf("telegram channel config: %w", err)
	}
	if c.ChatID == "" {
		return fmt.Errorf("telegram channel missing chat_id")
	}

	if provider != nil {
		var p struct {
			BotToken string `json:"bot_token"`
		}
		if err := json.Unmarshal(provider.Config, &p); err != nil {
			return fmt.Errorf("telegram provider config: %w", err)
		}
		if p.BotToken == "" {
			return fmt.Errorf("telegram provider missing bot_token")
		}
		return NewTelegramNotifier(p.BotToken).Send(ctx, payload, c.ChatID)
	}

	n, ok := fallback["telegram"]
	if !ok {
		return fmt.Errorf("no telegram provider or fallback configured")
	}
	return n.Send(ctx, payload, c.ChatID)
}

func sendEmail(ctx context.Context, payload model.NotificationPayload, chCfg json.RawMessage, provider *model.Provider, fallback Registry) error {
	var c struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal(chCfg, &c); err != nil {
		return fmt.Errorf("email channel config: %w", err)
	}
	if c.To == "" {
		return fmt.Errorf("email channel missing 'to'")
	}

	if provider != nil {
		var p struct {
			Host string `json:"host"`
			Port int    `json:"port"`
			User string `json:"user"`
			Pass string `json:"pass"`
			From string `json:"from"`
		}
		if err := json.Unmarshal(provider.Config, &p); err != nil {
			return fmt.Errorf("email provider config: %w", err)
		}
		if p.Port == 0 {
			p.Port = 587
		}
		return NewEmailNotifier(EmailConfig{
			Host: p.Host,
			Port: p.Port,
			User: p.User,
			Pass: p.Pass,
			From: p.From,
		}).Send(ctx, payload, c.To)
	}

	n, ok := fallback["email"]
	if !ok {
		return fmt.Errorf("no email provider or fallback configured")
	}
	return n.Send(ctx, payload, c.To)
}

func sendWebhook(ctx context.Context, payload model.NotificationPayload, chCfg json.RawMessage, provider *model.Provider, fallback Registry) error {
	var c struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(chCfg, &c); err != nil {
		return fmt.Errorf("webhook channel config: %w", err)
	}
	if c.URL == "" {
		return fmt.Errorf("webhook channel missing 'url'")
	}

	if provider != nil {
		var p struct {
			BaseURL        string `json:"base_url"`
			TimeoutSeconds int    `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(provider.Config, &p); err != nil {
			return fmt.Errorf("webhook provider config: %w", err)
		}
		return NewWebhookNotifier(p.BaseURL, p.TimeoutSeconds).Send(ctx, payload, c.URL)
	}

	if n, ok := fallback["webhook"]; ok {
		return n.Send(ctx, payload, c.URL)
	}
	// No provider and no fallback: try as absolute URL directly.
	return NewWebhookNotifier("", 5).Send(ctx, payload, c.URL)
}
