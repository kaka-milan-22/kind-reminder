package store

import (
"context"
"database/sql"
"encoding/json"
"errors"
"fmt"
"strings"
"time"

"crontab-reminder/internal/model"

_ "modernc.org/sqlite"
)

type Store struct {
db *sql.DB
}

func Open(dbPath string) (*Store, error) {
db, err := sql.Open("sqlite", dbPath)
if err != nil {
return nil, err
}
db.SetMaxOpenConns(1)
if err := initDB(db); err != nil {
_ = db.Close()
return nil, err
}
return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func initDB(db *sql.DB) error {
pragmas := []string{
"PRAGMA journal_mode=WAL;",
"PRAGMA synchronous=NORMAL;",
"PRAGMA foreign_keys=ON;",
"PRAGMA busy_timeout=5000;",
}
for _, p := range pragmas {
if _, err := db.Exec(p); err != nil {
return fmt.Errorf("apply %s: %w", p, err)
}
}
return runMigrations(db)
}

// --- Migration system ---

type migrationFn func(*sql.DB) error

type migration struct {
version int
fn      migrationFn
}

func runMigrations(db *sql.DB) error {
if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMP NOT NULL)`); err != nil {
return fmt.Errorf("create schema_migrations: %w", err)
}

var current int
_ = db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current)

migrations := []migration{
{1, migration1BaseSchema},
{2, migration2StepsTables},
{3, migration3RunningIndex},
{4, migration4DropLegacyJobColumns},
}

for _, m := range migrations {
if m.version <= current {
continue
}
if err := m.fn(db); err != nil {
return fmt.Errorf("migration %d: %w", m.version, err)
}
if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, m.version, time.Now().UTC()); err != nil {
return fmt.Errorf("record migration %d: %w", m.version, err)
}
}
return nil
}

func migration1BaseSchema(db *sql.DB) error {
stmts := []string{
`CREATE TABLE IF NOT EXISTS jobs (
id TEXT PRIMARY KEY,
cron_expr TEXT NOT NULL,
timezone TEXT NOT NULL,
title TEXT NOT NULL,
enabled INTEGER NOT NULL DEFAULT 1,
next_run_at TIMESTAMP NOT NULL,
last_run_at TIMESTAMP,
created_at TIMESTAMP NOT NULL,
updated_at TIMESTAMP NOT NULL
)`,
`CREATE TABLE IF NOT EXISTS executions (
id TEXT PRIMARY KEY,
job_id TEXT NOT NULL,
scheduled_at TIMESTAMP NOT NULL,
started_at TIMESTAMP,
finished_at TIMESTAMP,
status TEXT NOT NULL,
error TEXT,
UNIQUE(job_id, scheduled_at)
)`,
`CREATE TABLE IF NOT EXISTS channels (
id TEXT PRIMARY KEY,
type TEXT NOT NULL,
provider_id TEXT,
config TEXT NOT NULL,
created_at TIMESTAMP NOT NULL
)`,
`CREATE TABLE IF NOT EXISTS providers (
id TEXT PRIMARY KEY,
type TEXT NOT NULL,
config TEXT NOT NULL,
created_at TIMESTAMP NOT NULL
)`,
`CREATE INDEX IF NOT EXISTS idx_jobs_next_run_at ON jobs(next_run_at)`,
`CREATE INDEX IF NOT EXISTS idx_exec_job_sched ON executions(job_id, scheduled_at)`,
}
for _, s := range stmts {
if _, err := db.Exec(s); err != nil {
return err
}
}
return nil
}

func migration2StepsTables(db *sql.DB) error {
stmts := []string{
`CREATE TABLE IF NOT EXISTS job_steps (
id TEXT PRIMARY KEY,
job_id TEXT NOT NULL,
step_id TEXT NOT NULL,
order_index INTEGER NOT NULL,
type TEXT NOT NULL,
config_json TEXT NOT NULL,
timeout INTEGER NOT NULL DEFAULT 0,
retry INTEGER NOT NULL DEFAULT 0,
continue_on_error INTEGER NOT NULL DEFAULT 1,
UNIQUE(job_id, step_id),
UNIQUE(job_id, order_index),
FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
)`,
`CREATE TABLE IF NOT EXISTS execution_steps (
id TEXT PRIMARY KEY,
execution_id TEXT NOT NULL,
step_id TEXT NOT NULL,
type TEXT NOT NULL,
status TEXT NOT NULL,
started_at TIMESTAMP,
finished_at TIMESTAMP,
exit_code INTEGER NOT NULL DEFAULT 0,
stdout TEXT NOT NULL DEFAULT '',
stderr TEXT NOT NULL DEFAULT '',
error TEXT NOT NULL DEFAULT '',
FOREIGN KEY(execution_id) REFERENCES executions(id) ON DELETE CASCADE
)`,
`CREATE INDEX IF NOT EXISTS idx_execution_steps_exec ON execution_steps(execution_id)`,
}
for _, s := range stmts {
if _, err := db.Exec(s); err != nil {
return err
}
}
return nil
}

func migration3RunningIndex(db *sql.DB) error {
_, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_running_job ON executions(job_id) WHERE status='running' AND job_id != '__adhoc__'`)
return err
}

func migration4DropLegacyJobColumns(db *sql.DB) error {
cols := []string{"message", "channels_json"}
for _, col := range cols {
var count int
_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name=?`, col).Scan(&count)
if count > 0 {
if _, err := db.Exec(`ALTER TABLE jobs DROP COLUMN ` + col); err != nil {
return fmt.Errorf("drop column %s: %w", col, err)
}
}
}
return nil
}

// --- Job CRUD ---

func (s *Store) CreateJob(ctx context.Context, job *model.Job) error {
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
return err
}
defer tx.Rollback()

if _, err := tx.ExecContext(ctx, `
INSERT INTO jobs(id, cron_expr, timezone, title, enabled, next_run_at, last_run_at, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
`, job.ID, job.CronExpr, job.Timezone, job.Title, boolToInt(job.Enabled),
job.NextRunAt.UTC(), nullableTime(job.LastRunAt), job.CreatedAt.UTC(), job.UpdatedAt.UTC()); err != nil {
return err
}

if err := insertSteps(ctx, tx, job.ID, job.Steps); err != nil {
return err
}
return tx.Commit()
}

func (s *Store) GetJob(ctx context.Context, id string) (*model.Job, error) {
row := s.db.QueryRowContext(ctx, `
SELECT id, cron_expr, timezone, title, enabled, next_run_at, last_run_at, created_at, updated_at
FROM jobs WHERE id = ?
`, id)
job, err := scanJob(row)
if err != nil {
if errors.Is(err, sql.ErrNoRows) {
return nil, ErrNotFound
}
return nil, err
}
steps, err := s.getJobSteps(ctx, id)
if err != nil {
return nil, err
}
job.Steps = steps
return &job, nil
}

func (s *Store) ListJobs(ctx context.Context) ([]model.Job, error) {
rows, err := s.db.QueryContext(ctx, `
SELECT id, cron_expr, timezone, title, enabled, next_run_at, last_run_at, created_at, updated_at
FROM jobs ORDER BY created_at DESC
`)
if err != nil {
return nil, err
}
defer rows.Close()
out := make([]model.Job, 0)
for rows.Next() {
j, err := scanJob(rows)
if err != nil {
return nil, err
}
out = append(out, j)
}
return out, rows.Err()
}

func (s *Store) UpdateJob(ctx context.Context, job *model.Job) error {
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
return err
}
defer tx.Rollback()

res, err := tx.ExecContext(ctx, `
UPDATE jobs SET cron_expr=?, timezone=?, title=?, enabled=?, next_run_at=?, updated_at=?
WHERE id=?
`, job.CronExpr, job.Timezone, job.Title, boolToInt(job.Enabled), job.NextRunAt.UTC(), job.UpdatedAt.UTC(), job.ID)
if err != nil {
return err
}
n, err := res.RowsAffected()
if err != nil {
return err
}
if n == 0 {
return ErrNotFound
}

// Full replace steps if provided
if job.Steps != nil {
if _, err := tx.ExecContext(ctx, `DELETE FROM job_steps WHERE job_id=?`, job.ID); err != nil {
return err
}
if err := insertSteps(ctx, tx, job.ID, job.Steps); err != nil {
return err
}
}
return tx.Commit()
}

func (s *Store) DeleteJob(ctx context.Context, id string) error {
res, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id=?`, id)
if err != nil {
return err
}
n, _ := res.RowsAffected()
if n == 0 {
return ErrNotFound
}
return nil
}

func (s *Store) ListDueJobs(ctx context.Context, upTo time.Time) ([]model.Job, error) {
rows, err := s.db.QueryContext(ctx, `
SELECT id, cron_expr, timezone, title, enabled, next_run_at, last_run_at, created_at, updated_at
FROM jobs WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at ASC
`, upTo.UTC())
if err != nil {
return nil, err
}
defer rows.Close()
out := make([]model.Job, 0)
for rows.Next() {
j, err := scanJob(rows)
if err != nil {
return nil, err
}
out = append(out, j)
}
return out, rows.Err()
}

// --- Job Steps ---

func (s *Store) getJobSteps(ctx context.Context, jobID string) ([]model.Step, error) {
rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, step_id, order_index, type, config_json, timeout, retry, continue_on_error
FROM job_steps WHERE job_id=? ORDER BY order_index ASC
`, jobID)
if err != nil {
return nil, err
}
defer rows.Close()
return scanSteps(rows)
}

func insertSteps(ctx context.Context, tx *sql.Tx, jobID string, steps []model.Step) error {
for _, st := range steps {
cfg := "{}"
if len(st.Config) > 0 {
cfg = string(st.Config)
}
if _, err := tx.ExecContext(ctx, `
INSERT INTO job_steps(id, job_id, step_id, order_index, type, config_json, timeout, retry, continue_on_error)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
`, st.ID, jobID, st.StepID, st.OrderIndex, st.Type, cfg,
st.Timeout, st.Retry, boolToInt(st.ContinueOnError)); err != nil {
return err
}
}
return nil
}

func scanSteps(rows *sql.Rows) ([]model.Step, error) {
out := make([]model.Step, 0)
for rows.Next() {
var s model.Step
var cfg string
var cont int
if err := rows.Scan(&s.ID, &s.JobID, &s.StepID, &s.OrderIndex, &s.Type, &cfg, &s.Timeout, &s.Retry, &cont); err != nil {
return nil, err
}
s.Config = json.RawMessage(cfg)
s.ContinueOnError = cont == 1
out = append(out, s)
}
return out, rows.Err()
}

// --- Executions ---

// InsertRunningExecution atomically inserts an execution with status=running.
// Returns (false, nil) on unique constraint conflict (either same tick or already running).
func (s *Store) InsertRunningExecution(ctx context.Context, id, jobID string, scheduledAt time.Time) (bool, error) {
now := time.Now().UTC()
_, err := s.db.ExecContext(ctx, `
INSERT INTO executions(id, job_id, scheduled_at, started_at, status)
VALUES(?, ?, ?, ?, 'running')
`, id, jobID, scheduledAt.UTC(), now)
if err != nil {
if isUniqueViolation(err) {
return false, nil
}
return false, err
}
return true, nil
}

// InsertExecutionIfAbsent inserts an execution as pending.
// Deprecated: prefer InsertRunningExecution.
func (s *Store) InsertExecutionIfAbsent(ctx context.Context, executionID, jobID string, scheduledAt time.Time) (bool, error) {
_, err := s.db.ExecContext(ctx, `
INSERT INTO executions(id, job_id, scheduled_at, status) VALUES(?, ?, ?, 'pending')
`, executionID, jobID, scheduledAt.UTC())
if err != nil {
if isUniqueViolation(err) {
return false, nil
}
return false, err
}
return true, nil
}

func (s *Store) MarkExecutionRunning(ctx context.Context, id string, startedAt time.Time) error {
_, err := s.db.ExecContext(ctx, `UPDATE executions SET status='running', started_at=? WHERE id=?`, startedAt.UTC(), id)
return err
}

func (s *Store) MarkExecutionFinished(ctx context.Context, id string, status model.ExecutionStatus, finishedAt time.Time, errText string) error {
_, err := s.db.ExecContext(ctx, `UPDATE executions SET status=?, finished_at=?, error=? WHERE id=?`,
string(status), finishedAt.UTC(), nullableString(errText), id)
return err
}

// RecoverStaleExecutions marks any execution that was left in "running" state
// (e.g. from a previous crash or ungraceful restart) as failed. Returns the
// number of executions that were recovered.
func (s *Store) RecoverStaleExecutions(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE executions
SET status='failed', finished_at=?, error='execution interrupted: server restart'
WHERE status='running'`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *Store) AdvanceJobSchedule(ctx context.Context, jobID string, scheduledAt, nextRunAt time.Time) error {
_, err := s.db.ExecContext(ctx, `UPDATE jobs SET next_run_at=?, last_run_at=?, updated_at=? WHERE id=?`,
nextRunAt.UTC(), scheduledAt.UTC(), time.Now().UTC(), jobID)
return err
}

func (s *Store) FastForwardJobNextRun(ctx context.Context, jobID string, nextRunAt time.Time) error {
_, err := s.db.ExecContext(ctx, `UPDATE jobs SET next_run_at=?, updated_at=? WHERE id=?`,
nextRunAt.UTC(), time.Now().UTC(), jobID)
return err
}

// SaveExecutionStep saves a single step result. Truncates stdout/stderr to 8KB.
func (s *Store) SaveExecutionStep(ctx context.Context, es model.ExecutionStep) error {
_, err := s.db.ExecContext(ctx, `
INSERT INTO execution_steps(id, execution_id, step_id, type, status, started_at, finished_at, exit_code, stdout, stderr, error)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, es.ID, es.ExecutionID, es.StepID, es.Type, es.Status,
nullableTime(es.StartedAt), nullableTime(es.FinishedAt),
es.ExitCode, truncate(es.Stdout, 8192), truncate(es.Stderr, 8192), es.Error)
return err
}

type ListExecutionsOpts struct {
JobID        string
Limit        int
Offset       int
IncludeAdhoc bool
}

func (s *Store) ListExecutions(ctx context.Context, jobID string) ([]model.Execution, error) {
return s.ListExecutionsOpts(ctx, ListExecutionsOpts{JobID: jobID, Limit: 50})
}

func (s *Store) ListExecutionsOpts(ctx context.Context, opts ListExecutionsOpts) ([]model.Execution, error) {
if opts.Limit <= 0 {
opts.Limit = 50
}
if opts.Limit > 500 {
opts.Limit = 500
}

var conditions []string
var args []any
if strings.TrimSpace(opts.JobID) != "" {
conditions = append(conditions, "job_id=?")
args = append(args, opts.JobID)
}
if !opts.IncludeAdhoc {
conditions = append(conditions, "job_id != '__adhoc__'")
}
where := ""
if len(conditions) > 0 {
where = "WHERE " + strings.Join(conditions, " AND ")
}
args = append(args, opts.Limit, opts.Offset)
q := fmt.Sprintf(`SELECT id, job_id, scheduled_at, started_at, finished_at, status, error FROM executions %s ORDER BY scheduled_at DESC LIMIT ? OFFSET ?`, where)

rows, err := s.db.QueryContext(ctx, q, args...)
if err != nil {
return nil, err
}
defer rows.Close()

out := make([]model.Execution, 0)
for rows.Next() {
var e model.Execution
var startedAt, finishedAt sql.NullTime
var errText sql.NullString
if err := rows.Scan(&e.ID, &e.JobID, &e.ScheduledAt, &startedAt, &finishedAt, &e.Status, &errText); err != nil {
return nil, err
}
if startedAt.Valid {
e.StartedAt = startedAt.Time.UTC()
}
if finishedAt.Valid {
e.FinishedAt = finishedAt.Time.UTC()
}
if errText.Valid {
e.Error = errText.String
}
e.ScheduledAt = e.ScheduledAt.UTC()
out = append(out, e)
}
if err := rows.Err(); err != nil {
return nil, err
}

// Fetch steps for each execution
for i, e := range out {
steps, err := s.getExecutionSteps(ctx, e.ID)
if err != nil {
return nil, err
}
out[i].Steps = steps
}
return out, nil
}

func (s *Store) getExecutionSteps(ctx context.Context, executionID string) ([]model.ExecutionStep, error) {
rows, err := s.db.QueryContext(ctx, `
SELECT id, execution_id, step_id, type, status, started_at, finished_at, exit_code, stdout, stderr, error
FROM execution_steps WHERE execution_id=? ORDER BY rowid ASC
`, executionID)
if err != nil {
return nil, err
}
defer rows.Close()
out := make([]model.ExecutionStep, 0)
for rows.Next() {
var es model.ExecutionStep
var startedAt, finishedAt sql.NullTime
if err := rows.Scan(&es.ID, &es.ExecutionID, &es.StepID, &es.Type, &es.Status,
&startedAt, &finishedAt, &es.ExitCode, &es.Stdout, &es.Stderr, &es.Error); err != nil {
return nil, err
}
if startedAt.Valid {
es.StartedAt = startedAt.Time.UTC()
}
if finishedAt.Valid {
es.FinishedAt = finishedAt.Time.UTC()
}
out = append(out, es)
}
return out, rows.Err()
}

// --- Stats helpers ---

func (s *Store) CountEnabledJobs(ctx context.Context) (int, error) {
var n int
err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE enabled=1`).Scan(&n)
return n, err
}

func (s *Store) LastExecutionFinishedAt(ctx context.Context) (*time.Time, error) {
var finished sql.NullTime
err := s.db.QueryRowContext(ctx, `SELECT finished_at FROM executions WHERE finished_at IS NOT NULL ORDER BY finished_at DESC LIMIT 1`).Scan(&finished)
if err != nil {
if errors.Is(err, sql.ErrNoRows) {
return nil, nil
}
return nil, err
}
if !finished.Valid {
return nil, nil
}
t := finished.Time.UTC()
return &t, nil
}

func (s *Store) CountFailedExecutionsSince(ctx context.Context, since time.Time) (int, error) {
var n int
err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM executions WHERE status='failed' AND finished_at>?`, since.UTC()).Scan(&n)
return n, err
}

// --- Channel CRUD ---

func (s *Store) CreateChannelResource(ctx context.Context, ch *model.ChannelResource) error {
_, err := s.db.ExecContext(ctx,
`INSERT INTO channels(id, type, provider_id, config, created_at) VALUES(?, ?, ?, ?, ?)`,
ch.ID, string(ch.Type), nullableString(ch.ProviderID), string(ch.Config), ch.CreatedAt.UTC())
if err != nil {
if isUniqueViolation(err) {
return ErrConflict
}
}
return err
}

func (s *Store) GetChannelResource(ctx context.Context, id string) (*model.ChannelResource, error) {
var ch model.ChannelResource
var cfg string
var providerID sql.NullString
err := s.db.QueryRowContext(ctx, `SELECT id, type, provider_id, config, created_at FROM channels WHERE id=?`, id).
Scan(&ch.ID, &ch.Type, &providerID, &cfg, &ch.CreatedAt)
if err != nil {
if errors.Is(err, sql.ErrNoRows) {
return nil, ErrNotFound
}
return nil, err
}
ch.Config = json.RawMessage(cfg)
ch.CreatedAt = ch.CreatedAt.UTC()
if providerID.Valid {
ch.ProviderID = providerID.String
}
return &ch, nil
}

func (s *Store) ListChannelResources(ctx context.Context) ([]model.ChannelResource, error) {
rows, err := s.db.QueryContext(ctx, `SELECT id, type, provider_id, config, created_at FROM channels ORDER BY created_at DESC`)
if err != nil {
return nil, err
}
defer rows.Close()
out := make([]model.ChannelResource, 0)
for rows.Next() {
var ch model.ChannelResource
var cfg string
var providerID sql.NullString
if err := rows.Scan(&ch.ID, &ch.Type, &providerID, &cfg, &ch.CreatedAt); err != nil {
return nil, err
}
ch.Config = json.RawMessage(cfg)
ch.CreatedAt = ch.CreatedAt.UTC()
if providerID.Valid {
ch.ProviderID = providerID.String
}
out = append(out, ch)
}
return out, rows.Err()
}

func (s *Store) DeleteChannelResource(ctx context.Context, id string) error {
res, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id=?`, id)
if err != nil {
return err
}
n, _ := res.RowsAffected()
if n == 0 {
return ErrNotFound
}
return nil
}

// --- Provider CRUD ---

func (s *Store) CreateProvider(ctx context.Context, p *model.Provider) error {
_, err := s.db.ExecContext(ctx,
`INSERT INTO providers(id, type, config, created_at) VALUES(?, ?, ?, ?)`,
p.ID, string(p.Type), string(p.Config), p.CreatedAt.UTC())
if err != nil {
if isUniqueViolation(err) {
return ErrConflict
}
}
return err
}

func (s *Store) GetProvider(ctx context.Context, id string) (*model.Provider, error) {
var p model.Provider
var cfg string
err := s.db.QueryRowContext(ctx, `SELECT id, type, config, created_at FROM providers WHERE id=?`, id).
Scan(&p.ID, &p.Type, &cfg, &p.CreatedAt)
if err != nil {
if errors.Is(err, sql.ErrNoRows) {
return nil, ErrNotFound
}
return nil, err
}
p.Config = json.RawMessage(cfg)
p.CreatedAt = p.CreatedAt.UTC()
return &p, nil
}

func (s *Store) ListProviders(ctx context.Context) ([]model.Provider, error) {
rows, err := s.db.QueryContext(ctx, `SELECT id, type, config, created_at FROM providers ORDER BY created_at DESC`)
if err != nil {
return nil, err
}
defer rows.Close()
out := make([]model.Provider, 0)
for rows.Next() {
var p model.Provider
var cfg string
if err := rows.Scan(&p.ID, &p.Type, &cfg, &p.CreatedAt); err != nil {
return nil, err
}
p.Config = json.RawMessage(cfg)
p.CreatedAt = p.CreatedAt.UTC()
out = append(out, p)
}
return out, rows.Err()
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
res, err := s.db.ExecContext(ctx, `DELETE FROM providers WHERE id=?`, id)
if err != nil {
return err
}
n, _ := res.RowsAffected()
if n == 0 {
return ErrNotFound
}
return nil
}

// --- Helpers ---

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

func scanJob(scanner interface{ Scan(dest ...any) error }) (model.Job, error) {
var j model.Job
var enabled int
var lastRun sql.NullTime
if err := scanner.Scan(&j.ID, &j.CronExpr, &j.Timezone, &j.Title, &enabled, &j.NextRunAt, &lastRun, &j.CreatedAt, &j.UpdatedAt); err != nil {
return model.Job{}, err
}
j.Enabled = enabled == 1
j.NextRunAt = j.NextRunAt.UTC()
j.CreatedAt = j.CreatedAt.UTC()
j.UpdatedAt = j.UpdatedAt.UTC()
if lastRun.Valid {
j.LastRunAt = lastRun.Time.UTC()
}
return j, nil
}

func boolToInt(v bool) int {
if v {
return 1
}
return 0
}

func nullableString(v string) any {
if v == "" {
return nil
}
return v
}

func nullableTime(v time.Time) any {
if v.IsZero() {
return nil
}
return v.UTC()
}

func isUniqueViolation(err error) bool {
if err == nil {
return false
}
s := strings.ToLower(err.Error())
return strings.Contains(s, "unique") || strings.Contains(s, "constraint")
}

func truncate(s string, maxBytes int) string {
if len(s) <= maxBytes {
return s
}
return s[:maxBytes]
}

func (s *Store) ListOverdueEnabledJobs(ctx context.Context, now time.Time) ([]model.Job, error) {
return s.ListDueJobs(ctx, now)
}
