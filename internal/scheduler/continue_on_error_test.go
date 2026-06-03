package scheduler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"crontab-reminder/internal/model"
	"crontab-reminder/internal/notifier"
)

// With continue_on_error=true a failed step must NOT abort the run (later steps
// still execute), but it MUST still fail the execution — otherwise a failed
// notification would be silently reported as success.
func TestExecutionFailsWhenStepFailsDespiteContinueOnError(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(st, notifier.Registry{}, logger, Config{Workers: 1, QueueSize: 10})

	job := makeJob("job-coe", "*/1 * * * *", "UTC", time.Now().UTC())
	job.Steps = []model.Step{
		{ID: "s1", StepID: "fails", OrderIndex: 1, Type: "shell",
			Config: json.RawMessage(`{"script":"exit 7"}`), ContinueOnError: true},
		{ID: "s2", StepID: "after", OrderIndex: 2, Type: "shell",
			Config: json.RawMessage(`{"script":"echo ok"}`), ContinueOnError: true},
	}
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	execID := "exec-coe"
	if _, err := st.InsertRunningExecution(ctx, execID, job.ID, nil, model.TriggerTypeManual, "test", ""); err != nil {
		t.Fatalf("InsertRunningExecution: %v", err)
	}
	s.RunExecution(ctx, job, execID, nil, nil)

	e, err := st.GetExecution(ctx, execID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	// continue_on_error kept the run going: both steps executed.
	if len(e.Steps) != 2 {
		t.Fatalf("steps executed = %d, want 2 (continue_on_error must not abort the run)", len(e.Steps))
	}
	// ...but the failed step still fails the execution.
	if e.Status != model.ExecutionFailed {
		t.Fatalf("execution status = %q, want failed", e.Status)
	}
	if !strings.Contains(e.Error, "fails") {
		t.Fatalf("execution error %q should name the failed step", e.Error)
	}
}
