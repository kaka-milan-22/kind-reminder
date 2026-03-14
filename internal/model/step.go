package model

import (
"encoding/json"
"time"
)

type Step struct {
ID              string          `json:"id"`
JobID           string          `json:"job_id,omitempty"`
StepID          string          `json:"step_id"`
OrderIndex      int             `json:"order_index"`
Type            string          `json:"type"`
Config          json.RawMessage `json:"config"`
Timeout         int             `json:"timeout,omitempty"`
Retry           int             `json:"retry,omitempty"`
ContinueOnError bool            `json:"continue_on_error,omitempty"`
}

type StepResult struct {
Status   string `json:"status"` // "success" / "failed"
ExitCode int    `json:"exit_code,omitempty"`
Stdout   string `json:"stdout,omitempty"`
Stderr   string `json:"stderr,omitempty"`
Error    string `json:"error,omitempty"`
}

type RunContext struct {
ExecutionID string
JobID       string
Job         *Job
Timezone    string
ScheduledAt time.Time
Results     map[string]StepResult          // step_id → result
Overrides   map[string]map[string]any      // step_id → config overrides
}

type ExecutionStep struct {
ID          string    `json:"id"`
ExecutionID string    `json:"execution_id,omitempty"`
StepID      string    `json:"step_id"`
Type        string    `json:"type"`
Status      string    `json:"status"`
StartedAt   time.Time `json:"started_at,omitempty"`
FinishedAt  time.Time `json:"finished_at,omitempty"`
ExitCode    int       `json:"exit_code,omitempty"`
Stdout      string    `json:"stdout,omitempty"`
Stderr      string    `json:"stderr,omitempty"`
Error       string    `json:"error,omitempty"`
}
