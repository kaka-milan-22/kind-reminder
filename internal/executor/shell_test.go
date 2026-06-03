package executor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"crontab-reminder/internal/model"
)

func runShell(t *testing.T, script string) model.StepResult {
	t.Helper()
	cfg, _ := json.Marshal(shellConfig{Script: script})
	return NewShellExecutor().Execute(context.Background(), nil, model.Step{Config: cfg})
}

func TestShellRunsThroughShell(t *testing.T) {
	// A space-containing command only works if interpreted by the shell,
	// not exec'd as a single binary named "echo hi".
	res := runShell(t, "echo hi && echo bye")
	if res.Status != "success" {
		t.Fatalf("status = %q (err %q), want success", res.Status, res.Error)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := strings.TrimSpace(res.Stdout); got != "hi\nbye" {
		t.Fatalf("stdout = %q, want %q", got, "hi\nbye")
	}
}

func TestShellNonZeroExit(t *testing.T) {
	res := runShell(t, "exit 3")
	if res.Status != "failed" {
		t.Fatalf("status = %q, want failed", res.Status)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
}

func TestShellEmptyScript(t *testing.T) {
	res := runShell(t, "")
	if res.Status != "failed" {
		t.Fatalf("status = %q, want failed for empty script", res.Status)
	}
}

func TestCappedBufferTruncates(t *testing.T) {
	c := &cappedBuffer{max: 10}

	// Single write exceeding the cap is clipped, reports full length.
	n, err := c.Write([]byte("0123456789ABCDEF"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 16 {
		t.Fatalf("Write returned %d, want 16 (must report full length)", n)
	}
	if !c.truncated {
		t.Fatal("expected truncated=true")
	}

	// Further writes past the cap stay dropped but still report full length.
	if n, _ := c.Write([]byte("more")); n != 4 {
		t.Fatalf("Write returned %d, want 4", n)
	}

	got := c.String()
	if !strings.HasPrefix(got, "0123456789") {
		t.Fatalf("retained bytes = %q, want prefix %q", got, "0123456789")
	}
	if !strings.HasSuffix(got, "[output truncated]") {
		t.Fatalf("String() = %q, want truncation marker suffix", got)
	}
}

func TestCappedBufferUnderCap(t *testing.T) {
	c := &cappedBuffer{max: 64}
	c.Write([]byte("hello"))
	if c.truncated {
		t.Fatal("did not expect truncation")
	}
	if got := c.String(); got != "hello" {
		t.Fatalf("String() = %q, want %q", got, "hello")
	}
}
