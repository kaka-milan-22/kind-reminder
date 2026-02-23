package tmpl

import (
"bytes"
"fmt"
"text/template"
"time"

"crontab-reminder/internal/model"
)

type Context struct {
Job   *model.Job
Steps map[string]model.StepResult
Now   time.Time
}

// NowInTZ returns time.Now() in the given timezone. Falls back to UTC.
func NowInTZ(tz string) time.Time {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return time.Now().In(loc)
		}
	}
	return time.Now().UTC()
}

// NowInJobTZ returns time.Now() converted to the job's timezone.
func NowInJobTZ(job *model.Job) time.Time {
	if job != nil {
		return NowInTZ(job.Timezone)
	}
	return time.Now().UTC()
}

// Render renders a Go text/template string with the given context.
// Returns an error if the template fails to parse or execute.
func Render(tpl string, ctx Context) (string, error) {
if tpl == "" {
return "", nil
}
t, err := template.New("").Parse(tpl)
if err != nil {
return "", fmt.Errorf("template parse: %w", err)
}
data := map[string]any{
"job":   ctx.Job,
"steps": ctx.Steps,
"now":   ctx.Now,
}
var buf bytes.Buffer
if err := t.Execute(&buf, data); err != nil {
return "", fmt.Errorf("template execute: %w", err)
}
return buf.String(), nil
}
