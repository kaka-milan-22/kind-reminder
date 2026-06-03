package executor

import (
	"bytes"
	"context"
	"os/exec"

	"crontab-reminder/internal/model"
)

type ShellExecutor struct{}

func NewShellExecutor() *ShellExecutor { return &ShellExecutor{} }

type shellConfig struct {
	Script string `json:"script"`
}

// maxShellOutputBytes caps stdout/stderr captured per stream to avoid
// unbounded memory growth from a runaway command.
const maxShellOutputBytes = 64 << 10 // 64 KiB

// cappedBuffer is an io.Writer that retains at most max bytes and records
// whether any data was dropped. It always reports a full write so the
// running command never sees a short write / error.
type cappedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := c.max - c.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			c.buf.Write(p[:remaining])
			c.truncated = true
		} else {
			c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	if c.truncated {
		return c.buf.String() + "\n[output truncated]"
	}
	return c.buf.String()
}

func (e *ShellExecutor) Execute(ctx context.Context, _ *model.RunContext, step model.Step) model.StepResult {
	var cfg shellConfig
	if err := parseConfig(step.Config, &cfg); err != nil {
		return model.StepResult{Status: "failed", Error: "invalid shell config: " + err.Error()}
	}
	if cfg.Script == "" {
		return model.StepResult{Status: "failed", Error: "shell config: script is required"}
	}

	// Run the script through the shell so it supports full shell syntax
	// (pipes, &&, env expansion, builtins). The whole string is the program;
	// individual binaries are not invoked directly.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cfg.Script)
	stdout := &cappedBuffer{max: maxShellOutputBytes}
	stderr := &cappedBuffer{max: maxShellOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// context cancel, file not found, etc.
			return model.StepResult{
				Status:   "failed",
				ExitCode: -1,
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				Error:    err.Error(),
			}
		}
	}

	status := "success"
	if exitCode != 0 {
		status = "failed"
	}
	return model.StepResult{
		Status:   status,
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
}
