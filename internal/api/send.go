package api

import (
"context"
"encoding/json"
"errors"
"net/http"
"time"

"crontab-reminder/internal/executor"
"crontab-reminder/internal/model"
"crontab-reminder/internal/store"

"github.com/google/uuid"
)

type sendStepRequest struct {
StepID          string          `json:"step_id"`
OrderIndex      int             `json:"order_index"`
Type            string          `json:"type"`
Config          json.RawMessage `json:"config"`
Timeout         int             `json:"timeout"`
Retry           int             `json:"retry"`
ContinueOnError *bool           `json:"continue_on_error"`
}

type sendRequest struct {
Timezone string        `json:"timezone"`
Steps    []sendStepRequest `json:"steps"`
}

type sendResponse struct {
ExecutionID string               `json:"execution_id"`
Status      string               `json:"status"`
Steps       []model.ExecutionStep `json:"steps"`
}

// sendBackoffDurations mirrors the scheduler's retry backoff strategy.
var sendBackoffDurations = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

func sendBackoff(attempt int) time.Duration {
idx := attempt - 1
if idx < 0 {
return 0
}
if idx >= len(sendBackoffDurations) {
return sendBackoffDurations[len(sendBackoffDurations)-1]
}
return sendBackoffDurations[idx]
}

func (s *Server) sendHandler(w http.ResponseWriter, r *http.Request) {
var req sendRequest
if err := decodeJSON(r, &req); err != nil {
writeErr(w, http.StatusBadRequest, err)
return
}
if len(req.Steps) == 0 {
writeErr(w, http.StatusBadRequest, errors.New("steps is required"))
return
}

execID := uuid.NewString()

// Create adhoc execution
ok, err := s.store.InsertRunningExecution(r.Context(), execID, "__adhoc__", nil, model.TriggerTypeAdhoc, "", "")
if err != nil || !ok {
writeErr(w, http.StatusInternalServerError, errors.New("failed to create execution"))
return
}

execs := map[string]executor.Executor{
"shell":        executor.NewShellExecutor(),
"webhook":      executor.NewWebhookExecutor(),
"notification": executor.NewNotificationExecutor(s.store, s.notifiers),
}

runCtx := &model.RunContext{
ExecutionID: execID,
JobID:       "__adhoc__",
Timezone:    req.Timezone,
Results:     make(map[string]model.StepResult),
}

var stepRuns []model.ExecutionStep
finalStatus := model.ExecutionSuccess
var finalErr string

for _, sr := range req.Steps {
cfg := json.RawMessage("{}")
if len(sr.Config) > 0 {
cfg = sr.Config
}
step := model.Step{
ID:              uuid.NewString(),
StepID:          sr.StepID,
OrderIndex:      sr.OrderIndex,
Type:            sr.Type,
Config:          cfg,
Timeout:         sr.Timeout,
Retry:           sr.Retry,
ContinueOnError: sr.ContinueOnError == nil || *sr.ContinueOnError,
}

exec, ok := execs[step.Type]
if !ok {
res := model.StepResult{Status: "failed", Error: "unknown step type: " + step.Type}
es := saveStep(r.Context(), s.store, execID, step, res)
stepRuns = append(stepRuns, es)
runCtx.Results[step.StepID] = res
if !step.ContinueOnError {
finalStatus = model.ExecutionFailed
finalErr = res.Error
break
}
continue
}

maxAttempts := step.Retry
if maxAttempts < 1 {
maxAttempts = 1
}

var res model.StepResult
stepStart := time.Now().UTC()
for attempt := 1; attempt <= maxAttempts; attempt++ {
timeout := time.Duration(step.Timeout) * time.Second
if timeout <= 0 {
timeout = 5 * time.Minute
}
stepCtx, cancel := context.WithTimeout(r.Context(), timeout)
res = exec.Execute(stepCtx, runCtx, step)
cancel()
if res.Status == "success" {
break
}
if attempt < maxAttempts {
time.Sleep(sendBackoff(attempt))
}
}

es := saveStep(r.Context(), s.store, execID, step, res)
es.StartedAt = stepStart
stepRuns = append(stepRuns, es)
runCtx.Results[step.StepID] = res

if res.Status == "failed" && !step.ContinueOnError {
finalStatus = model.ExecutionFailed
finalErr = res.Error
break
}
}

_ = s.store.MarkExecutionFinished(r.Context(), execID, finalStatus, time.Now().UTC(), finalErr)

writeJSON(w, http.StatusOK, sendResponse{
ExecutionID: execID,
Status:      string(finalStatus),
Steps:       stepRuns,
})
}

func saveStep(ctx context.Context, st *store.Store, execID string, step model.Step, res model.StepResult) model.ExecutionStep {
now := time.Now().UTC()
es := model.ExecutionStep{
ID:          uuid.NewString(),
ExecutionID: execID,
StepID:      step.StepID,
Type:        step.Type,
Status:      res.Status,
StartedAt:   now,
FinishedAt:  now,
ExitCode:    res.ExitCode,
Stdout:      res.Stdout,
Stderr:      res.Stderr,
Error:       res.Error,
}
_ = st.SaveExecutionStep(ctx, es)
return es
}
