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

func (e *ShellExecutor) Execute(ctx context.Context, _ *model.RunContext, step model.Step) model.StepResult {
var cfg shellConfig
if err := parseConfig(step.Config, &cfg); err != nil {
return model.StepResult{Status: "failed", Error: "invalid shell config: " + err.Error()}
}
if cfg.Script == "" {
return model.StepResult{Status: "failed", Error: "shell config: script is required"}
}

cmd := exec.CommandContext(ctx, cfg.Script)
var stdout, stderr bytes.Buffer
cmd.Stdout = &stdout
cmd.Stderr = &stderr

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
