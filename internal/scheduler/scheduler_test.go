package scheduler

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"crontab-reminder/internal/model"
	"crontab-reminder/internal/notifier"
	"crontab-reminder/internal/store"
)

func TestRunOnceProcessesOnlyDueJobsAndUpdatesSchedule(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(st, notifier.Registry{}, logger, Config{TickInterval: time.Second, Workers: 1, QueueSize: 10})

	now := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	job := makeJob("job-due-boundary", "*/1 * * * *", "UTC", now.Add(time.Minute))
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	s.runOnce(ctx, now)
	execs, err := st.ListExecutions(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 0 {
		t.Fatalf("executions = %d, want 0", len(execs))
	}

	s.runOnce(ctx, now.Add(time.Minute))
	execs, err = st.ListExecutions(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("executions = %d, want 1", len(execs))
	}
	if !execs[0].ScheduledAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("scheduled_at = %s, want %s", execs[0].ScheduledAt, now.Add(time.Minute))
	}

	updated, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if !updated.LastRunAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("last_run_at = %s, want %s", updated.LastRunAt, now.Add(time.Minute))
	}
	if !updated.NextRunAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("next_run_at = %s, want %s", updated.NextRunAt, now.Add(2*time.Minute))
	}
}

func TestDispatchJobSkipsDuplicateExecution(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(st, notifier.Registry{}, logger, Config{TickInterval: time.Second, Workers: 1, QueueSize: 10})

	scheduled := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	job := makeJob("job-dup", "0 * * * *", "UTC", scheduled)
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	s.dispatchJob(ctx, *job, scheduled, scheduled)
	s.dispatchJob(ctx, *job, scheduled, scheduled)

	execs, err := st.ListExecutions(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("executions = %d, want 1", len(execs))
	}

	updated, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if !updated.NextRunAt.Equal(time.Date(2026, 2, 21, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_run_at = %s, want 2026-02-21 11:00:00 +0000 UTC", updated.NextRunAt)
	}
}

func TestRunOnceCatchesMissedRunsAndFastForwards(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(st, notifier.Registry{}, logger, Config{TickInterval: time.Second, Workers: 1, QueueSize: 10})

	job := makeJob(
		"job-skip-missed",
		"0 * * * *",
		"UTC",
		time.Date(2026, 2, 21, 9, 0, 0, 0, time.UTC),
	)
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// now is 10:00:30 → scheduledAt=09:00 → lateness=1h0m30s > default MaxLateness(1m) → skip
	s.runOnce(ctx, time.Date(2026, 2, 21, 10, 0, 30, 0, time.UTC))

	execs, err := st.ListExecutions(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 0 {
		t.Fatalf("executions = %d, want 0 (overdue job must be skipped in no-catchup mode)", len(execs))
	}

	updated, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	// next_run_at must be advanced to future (11:00), last_run_at must stay zero
	if !updated.NextRunAt.Equal(time.Date(2026, 2, 21, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_run_at = %s, want 2026-02-21 11:00:00 +0000 UTC", updated.NextRunAt)
	}
	if !updated.LastRunAt.IsZero() {
		t.Fatalf("last_run_at = %s, want zero (job was skipped)", updated.LastRunAt)
	}
}

func TestRunOnceTriggersCurrentMinuteOnly(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(st, notifier.Registry{}, logger, Config{TickInterval: time.Second, Workers: 1, QueueSize: 10})

	job := makeJob(
		"job-current-minute",
		"0 * * * *",
		"UTC",
		time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC),
	)
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	s.runOnce(ctx, time.Date(2026, 2, 21, 10, 0, 30, 0, time.UTC))

	execs, err := st.ListExecutions(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("executions = %d, want 1", len(execs))
	}
	if !execs[0].ScheduledAt.Equal(time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("scheduled_at = %s, want 2026-02-21 10:00:00 +0000 UTC", execs[0].ScheduledAt)
	}
}

// TestNoCatchupWithinTolerance verifies that a job slightly late (within MaxLateness) still executes.
func TestNoCatchupWithinTolerance(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// MaxLateness = 1m (default)
	s := New(st, notifier.Registry{}, logger, Config{TickInterval: time.Second, Workers: 1, QueueSize: 10})

	scheduled := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	job := makeJob("job-within-tolerance", "0 * * * *", "UTC", scheduled)
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// tick arrives 25s late → lateness=25s < 1m → must execute
	s.runOnce(ctx, scheduled.Add(25*time.Second))

	execs, err := st.ListExecutions(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("executions = %d, want 1 (within tolerance, must execute)", len(execs))
	}
}

// TestStartupRecoverySkipsOverdueJobs is the primary regression test for the
// no-catchup bug: service restarts hours late, stale next_run_at in DB must
// never trigger a catch-up execution.
func TestStartupRecoverySkipsOverdueJobs(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(st, notifier.Registry{}, logger, Config{TickInterval: time.Second, Workers: 1, QueueSize: 10})

	// Simulate: service was down, DB still holds next_run_at from hours ago.
	stalePast := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
	job := makeJob("job-startup-recovery", "0 */2 * * *", "UTC", stalePast)
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Service recovers at 17:34 — 3h34m after the stale scheduled time.
	restartNow := time.Date(2026, 2, 21, 17, 34, 0, 0, time.UTC)
	s.runOnce(ctx, restartNow)

	execs, err := st.ListExecutions(ctx, job.ID)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 0 {
		t.Fatalf("executions = %d, want 0 (startup must not catch up missed runs)", len(execs))
	}

	updated, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	// next_run_at must be advanced to the next future slot: 18:00
	want := time.Date(2026, 2, 21, 18, 0, 0, 0, time.UTC)
	if !updated.NextRunAt.Equal(want) {
		t.Fatalf("next_run_at = %s, want %s", updated.NextRunAt, want)
	}
	if !updated.LastRunAt.IsZero() {
		t.Fatalf("last_run_at = %s, want zero (nothing executed)", updated.LastRunAt)
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reminder.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func makeJob(id, cronExpr, tz string, next time.Time) *model.Job {
	now := time.Date(2026, 2, 21, 9, 0, 0, 0, time.UTC)
	return &model.Job{
		ID:        id,
		CronExpr:  cronExpr,
		Timezone:  tz,
		Title:     "test",
		Enabled:   true,
		NextRunAt: next.UTC(),
		CreatedAt: now,
		UpdatedAt: now,
	}
}
