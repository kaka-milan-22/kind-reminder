package api

import (
"context"
"encoding/json"
"errors"
"net/http"
"strconv"
"strings"
"time"

"crontab-reminder/internal/config"
cronx "crontab-reminder/internal/cron"
"crontab-reminder/internal/model"
"crontab-reminder/internal/notifier"
"crontab-reminder/internal/store"

"github.com/go-chi/chi/v5"
"github.com/google/uuid"
)

type Server struct {
store     *store.Store
apiToken  string
notifiers notifier.Registry
stats     StatsConfig
runner    JobRunner
}

type SchedulerStatusProvider interface {
IsRunning() bool
QueueLen() int
WorkerCount() int
QueueType() string
RateLimitPerSec() int
}

// JobRunner executes a job's steps for a given execution ID.
// The execution must already be inserted in the DB before calling Run.
type JobRunner interface {
RunExecution(ctx context.Context, job *model.Job, execID string, scheduledAt *time.Time, overrides map[string]map[string]any)
}

type StatsConfig struct {
TelegramToken string
SMTPHost      string
SMTPPort      int
Webhook       config.WebhookConfig
Scheduler     SchedulerStatusProvider
}

func New(st *store.Store, apiToken string, stats StatsConfig, notifiers notifier.Registry, runner JobRunner) *Server {
return &Server{store: st, apiToken: apiToken, stats: stats, notifiers: notifiers, runner: runner}
}

func (s *Server) Router() http.Handler {
r := chi.NewRouter()
r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
w.WriteHeader(http.StatusOK)
_, _ = w.Write([]byte("ok"))
})
r.Group(func(r chi.Router) {
r.Use(s.authMiddleware)
r.Get("/stats", s.statsHandler)
r.Post("/jobs", s.createJob)
r.Get("/jobs", s.listJobs)
r.Get("/jobs/{id}", s.getJob)
r.Patch("/jobs/{id}", s.patchJob)
r.Delete("/jobs/{id}", s.deleteJob)
r.Get("/executions", s.listExecutions)
r.Get("/executions/{id}", s.getExecutionHandler)
r.Post("/jobs/{id}/trigger", s.triggerJob)
r.Post("/providers", s.createProvider)
r.Get("/providers", s.listProviders)
r.Delete("/providers/{id}", s.deleteProvider)
r.Post("/channels", s.createChannel)
r.Get("/channels", s.listChannels)
r.Delete("/channels/{id}", s.deleteChannel)
r.Post("/send", s.sendHandler)
})
return r
}

// --- Request types ---

type stepRequest struct {
StepID          string          `json:"step_id"`
OrderIndex      int             `json:"order_index"`
Type            string          `json:"type"`
Config          json.RawMessage `json:"config"`
Timeout         int             `json:"timeout"`
Retry           int             `json:"retry"`
ContinueOnError *bool           `json:"continue_on_error"`
}

type createJobRequest struct {
Cron     string        `json:"cron"`
Timezone string        `json:"timezone"`
Title    string        `json:"title"`
Enabled  bool          `json:"enabled"`
Steps    []stepRequest `json:"steps"`
}

type patchJobRequest struct {
Cron     *string        `json:"cron"`
Timezone *string        `json:"timezone"`
Title    *string        `json:"title"`
Enabled  *bool          `json:"enabled"`
Steps    *[]stepRequest `json:"steps"`
}

func stepsFromRequest(reqs []stepRequest) []model.Step {
steps := make([]model.Step, 0, len(reqs))
for _, r := range reqs {
cfg := json.RawMessage("{}")
if len(r.Config) > 0 {
cfg = r.Config
}
steps = append(steps, model.Step{
ID:              uuid.NewString(),
StepID:          r.StepID,
OrderIndex:      r.OrderIndex,
Type:            r.Type,
Config:          cfg,
Timeout:         r.Timeout,
Retry:           r.Retry,
ContinueOnError: r.ContinueOnError == nil || *r.ContinueOnError,
})
}
return steps
}

// --- Handlers ---

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
var req createJobRequest
if err := decodeJSON(r, &req); err != nil {
writeErr(w, http.StatusBadRequest, err)
return
}
if err := validateJobRequest(req.Cron, req.Timezone, req.Title, req.Steps); err != nil {
writeErr(w, http.StatusBadRequest, err)
return
}
now := time.Now().UTC()
nextRun, err := cronx.NextRun(req.Cron, req.Timezone, now)
if err != nil {
writeErr(w, http.StatusBadRequest, err)
return
}
job := &model.Job{
ID:        uuid.NewString(),
CronExpr:  req.Cron,
Timezone:  req.Timezone,
Title:     req.Title,
Enabled:   req.Enabled,
NextRunAt: nextRun,
CreatedAt: now,
UpdatedAt: now,
Steps:     stepsFromRequest(req.Steps),
}
if err := s.store.CreateJob(r.Context(), job); err != nil {
writeErr(w, http.StatusInternalServerError, err)
return
}
writeJSON(w, http.StatusCreated, map[string]string{"id": job.ID})
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
jobs, err := s.store.ListJobs(r.Context())
if err != nil {
writeErr(w, http.StatusInternalServerError, err)
return
}
writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
id := chi.URLParam(r, "id")
job, err := s.store.GetJob(r.Context(), id)
if err != nil {
if errors.Is(err, store.ErrNotFound) {
writeErr(w, http.StatusNotFound, err)
return
}
writeErr(w, http.StatusInternalServerError, err)
return
}
writeJSON(w, http.StatusOK, job)
}

func (s *Server) patchJob(w http.ResponseWriter, r *http.Request) {
id := chi.URLParam(r, "id")
current, err := s.store.GetJob(r.Context(), id)
if err != nil {
if errors.Is(err, store.ErrNotFound) {
writeErr(w, http.StatusNotFound, err)
return
}
writeErr(w, http.StatusInternalServerError, err)
return
}

var req patchJobRequest
if err := decodeJSON(r, &req); err != nil {
writeErr(w, http.StatusBadRequest, err)
return
}

updated := *current
cronChanged, tzChanged, enabledChanged := false, false, false

if req.Cron != nil {
updated.CronExpr = strings.TrimSpace(*req.Cron)
cronChanged = true
}
if req.Timezone != nil {
updated.Timezone = strings.TrimSpace(*req.Timezone)
tzChanged = true
}
if req.Title != nil {
updated.Title = strings.TrimSpace(*req.Title)
}
if req.Enabled != nil {
enabledChanged = updated.Enabled != *req.Enabled
updated.Enabled = *req.Enabled
}
if req.Steps != nil {
updated.Steps = stepsFromRequest(*req.Steps)
} else {
updated.Steps = nil // don't replace steps if not provided
}

if err := validateJobFields(updated.CronExpr, updated.Timezone, updated.Title); err != nil {
writeErr(w, http.StatusBadRequest, err)
return
}

if updated.Enabled && (cronChanged || tzChanged || enabledChanged) {
nextRun, err := cronx.NextRun(updated.CronExpr, updated.Timezone, time.Now().UTC())
if err != nil {
writeErr(w, http.StatusBadRequest, err)
return
}
updated.NextRunAt = nextRun
}
updated.UpdatedAt = time.Now().UTC()

if err := s.store.UpdateJob(r.Context(), &updated); err != nil {
if errors.Is(err, store.ErrNotFound) {
writeErr(w, http.StatusNotFound, err)
return
}
writeErr(w, http.StatusInternalServerError, err)
return
}
// Re-fetch to include updated steps
result, err := s.store.GetJob(r.Context(), id)
if err != nil {
writeErr(w, http.StatusInternalServerError, err)
return
}
writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
id := chi.URLParam(r, "id")
if err := s.store.DeleteJob(r.Context(), id); err != nil {
if errors.Is(err, store.ErrNotFound) {
writeErr(w, http.StatusNotFound, err)
return
}
writeErr(w, http.StatusInternalServerError, err)
return
}
w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) {
q := r.URL.Query()
limit := parseInt(q.Get("limit"), 50)
offset := parseInt(q.Get("offset"), 0)
includeAdhoc := q.Get("include_adhoc") == "true"

execs, err := s.store.ListExecutionsOpts(r.Context(), store.ListExecutionsOpts{
JobID:        q.Get("job_id"),
Limit:        limit,
Offset:       offset,
IncludeAdhoc: includeAdhoc,
})
if err != nil {
writeErr(w, http.StatusInternalServerError, err)
return
}
writeJSON(w, http.StatusOK, execs)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
auth := r.Header.Get("Authorization")
if auth != "Bearer "+s.apiToken {
w.WriteHeader(http.StatusUnauthorized)
_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
return
}
next.ServeHTTP(w, r)
})
}

// --- Validation ---

func validateJobRequest(cron, tz, title string, steps []stepRequest) error {
if err := validateJobFields(cron, tz, title); err != nil {
return err
}
for _, st := range steps {
if strings.TrimSpace(st.StepID) == "" {
return errors.New("step_id is required")
}
if st.Type != "shell" && st.Type != "webhook" && st.Type != "notification" {
return errors.New("step type must be shell, webhook, or notification")
}
}
return nil
}

func validateJobFields(cron, tz, title string) error {
if strings.TrimSpace(cron) == "" {
return errors.New("cron is required")
}
if _, err := cronx.Parse(cron); err != nil {
return err
}
if err := cronx.ValidateTimezone(tz); err != nil {
return err
}
if strings.TrimSpace(title) == "" {
return errors.New("title is required")
}
return nil
}

// --- Helpers ---

func parseInt(s string, def int) int {
if s == "" {
return def
}
v, err := strconv.Atoi(s)
if err != nil || v < 0 {
return def
}
return v
}

func decodeJSON(r *http.Request, dst any) error {
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields()
return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(code)
_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
writeJSON(w, code, map[string]string{"error": err.Error()})
}
