package model

import (
"encoding/json"
"time"
)

type ChannelType string

const (
ChannelTelegram ChannelType = "telegram"
ChannelEmail    ChannelType = "email"
ChannelWebhook  ChannelType = "webhook"
)

type Job struct {
ID        string    `json:"id"`
CronExpr  string    `json:"cron"`
Timezone  string    `json:"timezone"`
Title     string    `json:"title"`
Enabled   bool      `json:"enabled"`
NextRunAt time.Time `json:"next_run_at"`
LastRunAt time.Time `json:"last_run_at,omitempty"`
CreatedAt time.Time `json:"created_at"`
UpdatedAt time.Time `json:"updated_at"`
Steps     []Step    `json:"steps,omitempty"`
}

type ChannelResource struct {
ID         string          `json:"id"`
Type       ChannelType     `json:"type"`
ProviderID string          `json:"provider_id,omitempty"`
Config     json.RawMessage `json:"config"`
CreatedAt  time.Time       `json:"created_at"`
}

type Provider struct {
ID        string          `json:"id"`
Type      ChannelType     `json:"type"`
Config    json.RawMessage `json:"config"`
CreatedAt time.Time       `json:"created_at"`
}

type ExecutionStatus string

const (
ExecutionPending ExecutionStatus = "pending"
ExecutionRunning ExecutionStatus = "running"
ExecutionSuccess ExecutionStatus = "success"
ExecutionFailed  ExecutionStatus = "failed"
)

type Execution struct {
ID          string          `json:"id"`
JobID       string          `json:"job_id"`
ScheduledAt time.Time       `json:"scheduled_at"`
StartedAt   time.Time       `json:"started_at,omitempty"`
FinishedAt  time.Time       `json:"finished_at,omitempty"`
Status      ExecutionStatus `json:"status"`
Error       string          `json:"error,omitempty"`
Steps       []ExecutionStep `json:"steps,omitempty"`
}
