package notifier

import (
	"fmt"
	"time"

	"crontab-reminder/internal/model"
)

const AppSignature = "From: App Kind Reminder"

func FormatNotification(p model.NotificationPayload) string {
	loc := time.UTC
	if stringsLoc, err := time.LoadLocation(p.Timezone); err == nil {
		loc = stringsLoc
	}

	scheduled := p.ScheduledAt.In(loc).Format("2006-01-02 15:04:05")
	triggered := p.TriggeredAt.In(loc).Format("2006-01-02 15:04:05")

	return fmt.Sprintf(
		`%s

%s

Job ID: %s
Execution: %s
Scheduled: %s
Triggered: %s

%s`,
		p.Title,
		p.Message,
		p.JobID,
		p.ExecutionID,
		scheduled,
		triggered,
		AppSignature,
	)
}
