package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"crontab-reminder/internal/model"
	"crontab-reminder/internal/notifier"
	"crontab-reminder/internal/store"
	"crontab-reminder/internal/tmpl"
)

type NotificationExecutor struct {
	store     *store.Store
	notifiers notifier.Registry
}

func NewNotificationExecutor(st *store.Store, reg notifier.Registry) *NotificationExecutor {
	return &NotificationExecutor{store: st, notifiers: reg}
}

type notificationConfig struct {
	Channels        []string `json:"channels"`
	TitleTemplate   string   `json:"title_template"`
	MessageTemplate string   `json:"message_template"`
}

func (e *NotificationExecutor) Execute(ctx context.Context, runCtx *model.RunContext, step model.Step) model.StepResult {
	var cfg notificationConfig
	if err := parseConfig(step.Config, &cfg); err != nil {
		return model.StepResult{Status: "failed", Error: "invalid notification config: " + err.Error()}
	}
	if len(cfg.Channels) == 0 {
		return model.StepResult{Status: "failed", Error: "notification config: channels is required"}
	}

	tplCtx := tmpl.Context{Job: runCtx.Job, Steps: runCtx.Results, Now: tmpl.NowInTZ(runCtx.Timezone)}
	message, err := tmpl.Render(cfg.MessageTemplate, tplCtx)
	if err != nil {
		return model.StepResult{Status: "failed", Error: "message template: " + err.Error()}
	}
	title, err := tmpl.Render(cfg.TitleTemplate, tplCtx)
	if err != nil {
		return model.StepResult{Status: "failed", Error: "title template: " + err.Error()}
	}

	payload := model.NotificationPayload{
		JobID:       runCtx.JobID,
		ExecutionID: runCtx.ExecutionID,
		Message:     message,
		ScheduledAt: runCtx.ScheduledAt,
		TriggeredAt: time.Now().UTC(),
	}
	payload.Timezone = runCtx.Timezone
	if runCtx.Job != nil {
		payload.Title = runCtx.Job.Title // fallback
	}
	if title != "" {
		payload.Title = title
	}

	var errs []error
	sent := 0
	for _, chID := range cfg.Channels {
		ch, err := e.store.GetChannelResource(ctx, chID)
		if err != nil {
			errs = append(errs, fmt.Errorf("channel %q: %w", chID, err))
			continue
		}
		var prov *model.Provider
		if ch.ProviderID != "" {
			prov, err = e.store.GetProvider(ctx, ch.ProviderID)
			if err != nil {
				errs = append(errs, fmt.Errorf("provider for channel %q: %w", chID, err))
				continue
			}
		}
		if err := notifier.SendViaChannelResource(ctx, payload, *ch, prov, e.notifiers); err != nil {
			errs = append(errs, fmt.Errorf("send to %q: %w", chID, err))
			continue
		}
		sent++
	}

	stdout := fmt.Sprintf("%d/%d channels sent", sent, len(cfg.Channels))
	if len(errs) > 0 {
		// Report every channel failure, not just the last one.
		return model.StepResult{Status: "failed", Stdout: stdout, Error: errors.Join(errs...).Error()}
	}
	return model.StepResult{Status: "success", Stdout: stdout}
}
