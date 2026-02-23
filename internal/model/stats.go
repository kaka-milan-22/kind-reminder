package model

import "time"

type Stats struct {
	SchedulerRunning bool `json:"scheduler_running"`

	JobsTotal int `json:"jobs_total"`

	QueueSize       int    `json:"queue_size"`
	Workers         int    `json:"workers"`
	QueueType       string `json:"queue_type"`
	RateLimitPerSec int    `json:"rate_limit_per_sec"`

	TelegramOK bool `json:"telegram_ok"`
	SMTPOK     bool `json:"smtp_ok"`
	WebhookOK  bool `json:"webhook_ok"`

	LastExecutionAt  *time.Time `json:"last_execution_at,omitempty"`
	FailuresLastHour int        `json:"failures_last_hour"`
}
