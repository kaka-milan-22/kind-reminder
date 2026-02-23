package model

import "time"

type NotificationPayload struct {
	Title       string
	Message     string
	JobID       string
	ExecutionID string
	ScheduledAt time.Time
	TriggeredAt time.Time
	Timezone    string
}
