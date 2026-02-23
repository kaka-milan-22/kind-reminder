package notifier

import (
	"strings"
	"testing"
	"time"

	"crontab-reminder/internal/model"
)

func TestFormatNotificationIncludesMetadataAndSignature(t *testing.T) {
	p := model.NotificationPayload{
		Title:       "写周报",
		Message:     "发送工作周报",
		JobID:       "job-1",
		ExecutionID: "exec-1",
		ScheduledAt: time.Date(2026, 2, 21, 1, 0, 0, 0, time.UTC),
		TriggeredAt: time.Date(2026, 2, 21, 1, 0, 5, 0, time.UTC),
		Timezone:    "Asia/Shanghai",
	}

	text := FormatNotification(p)
	for _, want := range []string{
		"写周报",
		"发送工作周报",
		"Job ID: job-1",
		"Execution: exec-1",
		"Scheduled: 2026-02-21 09:00:00",
		"Triggered: 2026-02-21 09:00:05",
		AppSignature,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("formatted text missing %q: %s", want, text)
		}
	}
}
