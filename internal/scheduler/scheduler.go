package scheduler

import (
"context"
"encoding/json"
"log/slog"
"sync/atomic"
"time"

cronx "crontab-reminder/internal/cron"
"crontab-reminder/internal/executor"
"crontab-reminder/internal/model"
"crontab-reminder/internal/notifier"
"crontab-reminder/internal/queue"
"crontab-reminder/internal/store"

"github.com/google/uuid"
)

type Config struct {
TickInterval    time.Duration
Workers         int
QueueSize       int
QueueType       string
RateLimitPerSec int
	MaxLateness     time.Duration
}

type task struct {
Job         model.Job
ExecutionID string
ScheduledAt *time.Time
Overrides   map[string]map[string]any
}

type Scheduler struct {
store     *store.Store
notifiers notifier.Registry
executors map[string]executor.Executor
cfg       Config
logger    *slog.Logger
dispatchQ queue.DispatchQueue[task]
running   atomic.Bool
}

func New(st *store.Store, regs notifier.Registry, logger *slog.Logger, cfg Config) *Scheduler {
if cfg.MaxLateness <= 0 {
		cfg.MaxLateness = time.Minute
	}
	if cfg.TickInterval <= 0 {
cfg.TickInterval = 30 * time.Second
}
if cfg.Workers <= 0 {
cfg.Workers = 10
}
if cfg.QueueSize <= 0 {
cfg.QueueSize = 100
}
if cfg.QueueType == "" {
cfg.QueueType = "memory"
}
var dispatchQ queue.DispatchQueue[task]
switch cfg.QueueType {
case "memory":
dispatchQ = queue.NewMemoryQueue[task](cfg.QueueSize, cfg.Workers, cfg.RateLimitPerSec)
default:
logger.Warn("unknown queue type, fallback to memory queue", "queue_type", cfg.QueueType)
dispatchQ = queue.NewMemoryQueue[task](cfg.QueueSize, cfg.Workers, cfg.RateLimitPerSec)
}

execs := map[string]executor.Executor{
"shell":        executor.NewShellExecutor(),
"webhook":      executor.NewWebhookExecutor(),
"notification": executor.NewNotificationExecutor(st, regs),
}

return &Scheduler{store: st, notifiers: regs, executors: execs, logger: logger, cfg: cfg, dispatchQ: dispatchQ}
}

func (s *Scheduler) Start(ctx context.Context) {
s.running.Store(true)
defer s.running.Store(false)

// Recover executions left in "running" state from a previous crash or restart.
if n, err := s.store.RecoverStaleExecutions(ctx); err != nil {
s.logger.Error("recover stale executions failed", "error", err)
} else if n > 0 {
s.logger.Warn("recovered stale executions on startup", "count", n)
}

s.dispatchQ.StartWorkers(ctx, func(c context.Context, t task) error {
s.handleTask(c, t)
return nil
})

s.runOnce(ctx, time.Now().UTC())
ticker := time.NewTicker(s.cfg.TickInterval)
defer ticker.Stop()

for {
select {
case <-ctx.Done():
return
case <-ticker.C:
s.runOnce(ctx, time.Now().UTC())
}
}
}

func (s *Scheduler) IsRunning() bool      { return s.running.Load() }
func (s *Scheduler) QueueLen() int        { return s.dispatchQ.Len() }
func (s *Scheduler) WorkerCount() int     { return s.dispatchQ.Workers() }
func (s *Scheduler) QueueType() string    { return s.dispatchQ.Type() }
func (s *Scheduler) RateLimitPerSec() int { return s.dispatchQ.RateLimitPerSec() }

func (s *Scheduler) runOnce(ctx context.Context, nowUTC time.Time) {
nowMinute := nowUTC.UTC().Truncate(time.Minute)
jobs, err := s.store.ListDueJobs(ctx, nowMinute)
if err != nil {
s.logger.Error("list due jobs failed", "error", err)
return
}
for _, job := range jobs {
s.dispatchJob(ctx, job, job.NextRunAt, nowUTC)
}
}

func (s *Scheduler) dispatchJob(ctx context.Context, job model.Job, scheduledAt time.Time, nowUTC time.Time) {
scheduledAt = scheduledAt.UTC()
execID := uuid.NewString()

// NextRunAfter loops cron.Next until result > nowUTC, so nextRunAt is always
	// strictly in the future — never cron.Next(scheduledAt) which could still be past.
nextRunAt, err := cronx.NextRunAfter(job.CronExpr, job.Timezone, scheduledAt, nowUTC)
if err != nil {
s.logger.Error("calculate next run failed", "job_id", job.ID, "error", err)
return
}

	// No-catchup: compute lateness in job's own timezone so DST transitions
	// and cross-timezone deployments don't skew the comparison.
	jobLoc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		jobLoc = time.UTC // timezone already validated at job creation; fallback is safe
	}
	lateness := nowUTC.In(jobLoc).Sub(scheduledAt.In(jobLoc))
	if lateness > s.cfg.MaxLateness {
		s.logger.Info("job overdue, skipping (no-catchup)", "job_id", job.ID, "scheduled_at", scheduledAt, "lateness", lateness)
		if err := s.store.FastForwardJobNextRun(ctx, job.ID, nextRunAt); err != nil {
			s.logger.Error("advance job schedule failed", "job_id", job.ID, "error", err)
		}
		return
	}

	// InsertRunningExecution handles both dedup (UNIQUE job_id+scheduled_at)
// and concurrent-run prevention (partial unique index on status=running).
created, err := s.store.InsertRunningExecution(ctx, execID, job.ID, &scheduledAt, model.TriggerTypeCron, "system", "")
if err != nil {
s.logger.Error("insert running execution failed", "job_id", job.ID, "error", err)
return
}
if !created {
// Already dispatched or running for this scheduled_at.
// Still advance next_run_at so the job is not stuck in ListDueJobs indefinitely.
s.logger.Info("job skipped (already running or same tick)", "job_id", job.ID)
if err := s.store.FastForwardJobNextRun(ctx, job.ID, nextRunAt); err != nil {
s.logger.Error("advance job schedule failed", "job_id", job.ID, "error", err)
}
return
}

if err := s.store.AdvanceJobSchedule(ctx, job.ID, scheduledAt, nextRunAt); err != nil {
s.logger.Error("advance job schedule failed", "job_id", job.ID, "error", err)
return
}

if err := s.dispatchQ.Push(ctx, task{Job: job, ExecutionID: execID, ScheduledAt: &scheduledAt}); err != nil {
s.logger.Error("enqueue job failed", "job_id", job.ID, "error", err)
} else {
s.logger.Info("job triggered", "job_id", job.ID, "scheduled_at", scheduledAt)
}
}

// backoffDurations returns the wait time before the nth retry (1-indexed).
var backoffDurations = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

func backoff(attempt int) time.Duration {
idx := attempt - 1
if idx < 0 {
return 0
}
if idx >= len(backoffDurations) {
return backoffDurations[len(backoffDurations)-1]
}
return backoffDurations[idx]
}

func (s *Scheduler) handleTask(ctx context.Context, t task) {
var scheduledAt time.Time
if t.ScheduledAt != nil {
scheduledAt = *t.ScheduledAt
}
s.runJob(ctx, &t.Job, t.ExecutionID, scheduledAt, t.Overrides)
}

func (s *Scheduler) runJob(ctx context.Context, jobSnapshot *model.Job, execID string, scheduledAt time.Time, overrides map[string]map[string]any) {
log := s.logger.With("job_id", jobSnapshot.ID, "execution_id", execID)
log.Info("execution started")

// Load job steps from DB (authoritative, not from snapshot)
job, err := s.store.GetJob(ctx, jobSnapshot.ID)
if err != nil {
log.Error("load job failed", "error", err)
_ = s.store.MarkExecutionFinished(ctx, execID, model.ExecutionFailed, time.Now().UTC(), "load job: "+err.Error())
return
}

runCtx := &model.RunContext{
ExecutionID: execID,
JobID:       jobSnapshot.ID,
Job:         job,
Timezone:    job.Timezone,
ScheduledAt: scheduledAt,
Results:     make(map[string]model.StepResult),
Overrides:   overrides,
}

execFailed := false
var execErr string

for _, step := range job.Steps {
exec, ok := s.executors[step.Type]
if !ok {
log.Error("unknown step type", "step_id", step.StepID, "type", step.Type)
res := model.StepResult{Status: "failed", Error: "unknown step type: " + step.Type}
runCtx.Results[step.StepID] = res
s.saveExecutionStep(ctx, execID, step, res, time.Now().UTC(), time.Now().UTC())
if !step.ContinueOnError {
execFailed = true
execErr = res.Error
break
}
continue
}

// Apply config overrides for this step (merge non-null config fields only)
if overrides != nil {
if ov, ok := overrides[step.StepID]; ok {
step = applyStepOverride(step, ov)
}
}

maxAttempts := step.Retry
if maxAttempts < 1 {
maxAttempts = 1
}

var res model.StepResult
var stepStart time.Time
for attempt := 1; attempt <= maxAttempts; attempt++ {
stepStart = time.Now().UTC()

timeout := time.Duration(step.Timeout) * time.Second
if timeout <= 0 {
timeout = 5 * time.Minute
}
stepCtx, cancel := context.WithTimeout(ctx, timeout)
res = exec.Execute(stepCtx, runCtx, step)
cancel()

log.Info("step attempt", "step_id", step.StepID, "attempt", attempt, "status", res.Status)

if res.Status == "success" {
break
}
if attempt < maxAttempts {
time.Sleep(backoff(attempt))
}
}

stepEnd := time.Now().UTC()
s.saveExecutionStep(ctx, execID, step, res, stepStart, stepEnd)
runCtx.Results[step.StepID] = res

if res.Status == "failed" && !step.ContinueOnError {
execFailed = true
execErr = res.Error
break
}
}

finished := time.Now().UTC()
if execFailed {
log.Error("execution failed", "error", execErr)
_ = s.store.MarkExecutionFinished(ctx, execID, model.ExecutionFailed, finished, execErr)
} else {
log.Info("execution success")
_ = s.store.MarkExecutionFinished(ctx, execID, model.ExecutionSuccess, finished, "")
}
}

// applyStepOverride merges override map into step's Config JSON.
// Blocked fields (type, order_index, step_id) are ignored.
// Null values are ignored.
func applyStepOverride(step model.Step, overrides map[string]any) model.Step {
if len(overrides) == 0 {
return step
}
blocked := map[string]bool{"type": true, "order_index": true, "step_id": true}
base := make(map[string]any)
if len(step.Config) > 0 {
_ = json.Unmarshal(step.Config, &base)
}
for k, v := range overrides {
if blocked[k] {
continue
}
if v == nil {
continue // skip null values
}
base[k] = v
}
merged, err := json.Marshal(base)
if err == nil {
step.Config = merged
}
return step
}

// RunExecution runs a job's steps and marks the execution finished.
// The execution must already be inserted (status=running) before calling this.
// This method is safe to call from a goroutine.
func (s *Scheduler) RunExecution(ctx context.Context, job *model.Job, execID string, scheduledAt *time.Time, overrides map[string]map[string]any) {
var schedTime time.Time
if scheduledAt != nil {
schedTime = *scheduledAt
}
s.runJob(ctx, job, execID, schedTime, overrides)
}


func (s *Scheduler) saveExecutionStep(ctx context.Context, execID string, step model.Step, res model.StepResult, started, finished time.Time) {
es := model.ExecutionStep{
ID:          uuid.NewString(),
ExecutionID: execID,
StepID:      step.StepID,
Type:        step.Type,
Status:      res.Status,
StartedAt:   started,
FinishedAt:  finished,
ExitCode:    res.ExitCode,
Stdout:      res.Stdout,
Stderr:      res.Stderr,
Error:       res.Error,
}
if err := s.store.SaveExecutionStep(ctx, es); err != nil {
s.logger.Error("save execution step failed", "execution_id", execID, "step_id", step.StepID, "error", err)
}
}
