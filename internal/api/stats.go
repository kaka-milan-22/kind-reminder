package api

import (
	"context"
	"net"
	"net/http"
	"time"

	"crontab-reminder/internal/model"
)

func (s *Server) statsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobsTotal, err := s.store.CountEnabledJobs(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	lastExecutionAt, err := s.store.LastExecutionFinishedAt(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	failuresLastHour, err := s.store.CountFailedExecutionsSince(ctx, time.Now().UTC().Add(-1*time.Hour))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	out := model.Stats{
		SchedulerRunning: s.stats.Scheduler != nil && s.stats.Scheduler.IsRunning(),
		JobsTotal:        jobsTotal,
		QueueSize:        schedulerQueueSize(s.stats.Scheduler),
		Workers:          schedulerWorkers(s.stats.Scheduler),
		QueueType:        schedulerQueueType(s.stats.Scheduler),
		RateLimitPerSec:  schedulerRateLimitPerSec(s.stats.Scheduler),
		TelegramOK:       checkTelegram(ctx, s.stats.TelegramToken),
		SMTPOK:           checkSMTP(ctx, s.stats.SMTPHost, s.stats.SMTPPort),
		WebhookOK:        checkWebhook(ctx, s.stats.Webhook.Enabled, s.stats.Webhook.BaseURL, s.stats.Webhook.Timeout),
		LastExecutionAt:  lastExecutionAt,
		FailuresLastHour: failuresLastHour,
	}
	writeJSON(w, http.StatusOK, out)
}

func schedulerQueueSize(s SchedulerStatusProvider) int {
	if s == nil {
		return 0
	}
	return s.QueueLen()
}

func schedulerWorkers(s SchedulerStatusProvider) int {
	if s == nil {
		return 0
	}
	return s.WorkerCount()
}

func schedulerQueueType(s SchedulerStatusProvider) string {
	if s == nil {
		return ""
	}
	return s.QueueType()
}

func schedulerRateLimitPerSec(s SchedulerStatusProvider) int {
	if s == nil {
		return 0
	}
	return s.RateLimitPerSec()
}

func checkTelegram(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	url := "https://api.telegram.org/bot" + token + "/getMe"
	return httpOK(ctx, http.MethodGet, url, 5*time.Second)
}

func checkSMTP(ctx context.Context, host string, port int) bool {
	if host == "" || port <= 0 {
		return false
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkWebhook(ctx context.Context, enabled bool, baseURL string, timeoutSec int) bool {
	if !enabled || baseURL == "" {
		return false
	}
	timeout := 5 * time.Second
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}
	base := trimSlash(baseURL)
	if httpOK(ctx, http.MethodHead, base+"/health", timeout) {
		return true
	}
	return httpOK(ctx, http.MethodGet, base+"/", timeout)
}

func httpOK(ctx context.Context, method, url string, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func trimSlash(v string) string {
	for len(v) > 0 && v[len(v)-1] == '/' {
		v = v[:len(v)-1]
	}
	return v
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return sign + string(buf[i:])
}
