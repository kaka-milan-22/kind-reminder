package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crontab-reminder/internal/api"
	"crontab-reminder/internal/config"
	"crontab-reminder/internal/notifier"
	"crontab-reminder/internal/scheduler"
	"crontab-reminder/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		panic(err)
	}
	defer st.Close()

	notifiers := notifier.Registry{}
	if cfg.TelegramBotToken != "" {
		notifiers["telegram"] = notifier.NewTelegramNotifier(cfg.TelegramBotToken)
	}
	if cfg.SMTPHost != "" && cfg.SMTPFrom != "" {
		notifiers["email"] = notifier.NewEmailNotifier(notifier.EmailConfig{
			Host: cfg.SMTPHost,
			Port: cfg.SMTPPort,
			User: cfg.SMTPUser,
			Pass: cfg.SMTPPass,
			From: cfg.SMTPFrom,
		})
	}
	if cfg.Webhook.Enabled {
		notifiers["webhook"] = notifier.NewWebhookNotifier(cfg.Webhook.BaseURL, cfg.Webhook.Timeout)
	}

	s := scheduler.New(st, notifiers, logger, scheduler.Config{
		TickInterval:    30 * time.Second,
		Workers:         cfg.QueueConfig.Workers,
		QueueSize:       cfg.QueueConfig.Size,
		QueueType:       cfg.QueueConfig.Type,
		RateLimitPerSec: cfg.QueueConfig.RateLimitPerSec,
		MaxLateness:     cfg.SchedulerMaxLateness,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go s.Start(ctx)

	apiServer := api.New(st, cfg.APIToken, api.StatsConfig{
		TelegramToken: cfg.TelegramBotToken,
		SMTPHost:      cfg.SMTPHost,
		SMTPPort:      cfg.SMTPPort,
		Webhook:       cfg.Webhook,
		Scheduler:     s,
	}, notifiers)
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.ServerPort),
		Handler:      apiServer.Router(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("server started", "port", cfg.ServerPort)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
