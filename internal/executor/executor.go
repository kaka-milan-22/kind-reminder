package executor

import (
"context"

"crontab-reminder/internal/model"
)

// Executor executes a single step attempt. It does NOT implement retry.
// Retry is handled by the Runner.
type Executor interface {
Execute(ctx context.Context, runCtx *model.RunContext, step model.Step) model.StepResult
}
