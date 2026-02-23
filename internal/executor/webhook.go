package executor

import (
"context"
"fmt"
"io"
"net/http"
"strings"
"time"

"crontab-reminder/internal/model"
"crontab-reminder/internal/tmpl"
)

type WebhookExecutor struct{}

func NewWebhookExecutor() *WebhookExecutor { return &WebhookExecutor{} }

type webhookConfig struct {
Method       string            `json:"method"`
URL          string            `json:"url"`
Headers      map[string]string `json:"headers"`
BodyTemplate string            `json:"body_template"`
}

func (e *WebhookExecutor) Execute(ctx context.Context, runCtx *model.RunContext, step model.Step) model.StepResult {
var cfg webhookConfig
if err := parseConfig(step.Config, &cfg); err != nil {
return model.StepResult{Status: "failed", Error: "invalid webhook config: " + err.Error()}
}
if cfg.URL == "" {
return model.StepResult{Status: "failed", Error: "webhook config: url is required"}
}
if cfg.Method == "" {
cfg.Method = "POST"
}

tplCtx := tmpl.Context{Job: runCtx.Job, Steps: runCtx.Results, Now: tmpl.NowInTZ(runCtx.Timezone)}

// Render body
body, err := tmpl.Render(cfg.BodyTemplate, tplCtx)
if err != nil {
return model.StepResult{Status: "failed", Error: "body template: " + err.Error()}
}

req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, strings.NewReader(body))
if err != nil {
return model.StepResult{Status: "failed", Error: "create request: " + err.Error()}
}
if body != "" {
req.Header.Set("Content-Type", "application/json")
}

// Render and set headers
for k, v := range cfg.Headers {
rendered, err := tmpl.Render(v, tplCtx)
if err != nil {
return model.StepResult{Status: "failed", Error: fmt.Sprintf("header %q template: %s", k, err)}
}
req.Header.Set(k, rendered)
}

client := &http.Client{Timeout: 10 * time.Second}
resp, err := client.Do(req)
if err != nil {
return model.StepResult{Status: "failed", Error: "request: " + err.Error()}
}
defer resp.Body.Close()

respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
status := "success"
if resp.StatusCode >= 400 {
status = "failed"
}
return model.StepResult{
Status: status,
Stdout: fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(respBody)),
}
}
