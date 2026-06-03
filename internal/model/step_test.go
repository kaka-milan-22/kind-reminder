package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// A step set to continue_on_error=false must serialize the field, not drop it.
// Otherwise a GET→PATCH round-trip (steps fully replaced) would re-default it to
// true and silently change behavior.
func TestStepSerializesContinueOnErrorFalse(t *testing.T) {
	b, err := json.Marshal(Step{StepID: "s1", Type: "shell", ContinueOnError: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"continue_on_error":false`) {
		t.Fatalf("missing continue_on_error:false in %s", b)
	}
}

// Exit code 0 (success) is meaningful and must appear, not be dropped.
func TestStepResultSerializesExitCodeZero(t *testing.T) {
	b, err := json.Marshal(StepResult{Status: "success", ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"exit_code":0`) {
		t.Fatalf("missing exit_code:0 in %s", b)
	}
}

// Unset times serialize as omitted (null/absent), not the zero "0001-01-01".
func TestExecutionStepOmitsNilTimes(t *testing.T) {
	b, err := json.Marshal(ExecutionStep{StepID: "s1", Type: "shell", Status: "success"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "0001-01-01") {
		t.Fatalf("zero time leaked into %s", b)
	}
}
