package notifier

import (
	"context"

	"crontab-reminder/internal/model"
)

type Notifier interface {
	Send(ctx context.Context, payload model.NotificationPayload, target string) error
}

type Registry map[string]Notifier
