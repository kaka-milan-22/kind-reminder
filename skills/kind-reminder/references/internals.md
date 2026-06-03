# kind-reminder internals & behavior

Read this when debugging *why* something happened (a job skipped, a worker stuck,
duplicate/missing runs). Package layout under `internal/`: `scheduler`, `queue`,
`executor` (shell/webhook/notification), `store` (sqlite), `notifier`
(telegram/email/webhook), `config`, `cron`, `tmpl`, `model`, `api`.

## Scheduler

- Single goroutine ticks every **30s** (`TickInterval`), plus once at startup.
  Each tick: `ListDueJobs` (enabled, `next_run_at <= now`) → dispatch each.
- **No-catchup:** if a job is overdue by more than `scheduler.max_lateness`
  (default 1m), it is **skipped**, not run, and `next_run_at` fast-forwarded. This
  prevents a backlog stampede after downtime. Lateness is computed in the job's
  own timezone (DST-safe). → "job overdue, skipping (no-catchup)" in logs.
- **Dedup / no double-run:** `InsertRunningExecution` relies on two SQLite
  constraints — `UNIQUE(job_id, scheduled_at)` and a partial unique index
  `uniq_running_job` on `status='running'` (excluding `__adhoc__`). A conflict
  means "already dispatched / already running" → skip + fast-forward, not an error.
- **Crash recovery:** on startup `RecoverStaleExecutions` flips any execution
  left in `running` (from an ungraceful stop) to `failed`. → "recovered stale
  executions on startup".
- **next_run_at** is always computed strictly in the future (`NextRunAfter` loops
  cron.Next past `now`), so a slow tick can't wedge a job in the due set.

## Queue & workers

- In-memory buffered channel (`queue.size`) consumed by `queue.workers`
  goroutines. `queue.type` only supports `memory`; `redis` is a stub that falls
  back to memory with a warning.
- **Rate limit is global:** all workers share one ticker, so `rate_limit_per_sec`
  is a true per-second cap across the pool (not per worker). 0 disables it.
- `workers` = max concurrent job executions; `size` = max queued-but-not-started
  backlog. A full queue blocks dispatch (back-pressure on the tick loop).

## Step execution

- Each step runs under a ctx with timeout = `step.timeout` seconds (default 300).
- **Retry:** up to `step.retry` attempts, backoff 2s → 5s → 10s between tries.
  (Backoff uses `time.Sleep`, so it doesn't abort early on shutdown.)
- `continue_on_error` (default true) decides only whether a failed step aborts the
  rest of the job. It does NOT mask the failure: the execution is marked `failed`
  if **any** step failed (the execution error aggregates all failed steps as
  `stepID: error; ...`). This keeps a failed notification from being reported as a
  successful run.
- The job's steps are reloaded from the DB at run time (authoritative), not from
  the dispatch snapshot.

### shell
Runs `/bin/sh -c "<script>"` — full shell syntax (pipes, `&&`, builtins, env).
stdout/stderr are captured into a capped buffer (**64KB** each in memory; an
`[output truncated]` marker is appended past that), and **persisted to 8KB** in
`execution_steps`. A capped writer always reports full writes so the command never
sees a short-write error.

### webhook
`http.NewRequestWithContext` with the step ctx; **no fixed client timeout** (the
ctx governs). Body and header values are Go templates. Response body is read up to
8KB; status ≥ 400 = failed; stdout is `HTTP <code>\n<body>`.

### notification
Sends to each channel in `config.channels`. Per-channel failures are collected and
reported together via `errors.Join` (every `send to "X": ...` shown, not just the
last). stdout summarizes `N/M channels sent`. A channel may bind a Provider
(its own token/SMTP creds); otherwise it falls back to the server-level notifier
registry built from env/config.

## Notifiers

- **Telegram:** `POST .../sendMessage`, 5s client timeout, bot token from
  `TELEGRAM_BOT_TOKEN` (vault).
- **Email (SMTP):** dials with ctx, then **sets a connection deadline** from the
  ctx (net/smtp itself ignores context) so a stalling server can't hang a worker
  past the step timeout; falls back to 30s if ctx has no deadline. STARTTLS + AUTH
  when advertised. Password from `SMTP_PASS` (vault).
- **SMTP diagnostics** (`POST /diagnostics/smtp`) walks DNS→TCP→greeting→EHLO→
  STARTTLS→AUTH→MAIL→RCPT and reports the last good stage — use it to localize
  email failures.

## Store / migrations

- SQLite, `MaxOpenConns(1)` (serialized writer), WAL, `busy_timeout=5000`,
  foreign keys on. All queries parameterized.
- Versioned migrations run on startup; current schema is **v5**. Tables: `jobs`,
  `job_steps`, `executions`, `execution_steps`, `channels`, `providers`,
  `schema_migrations`. Migrations are **not yet wrapped in a transaction** — a
  crash mid-migration (notably the v5 executions-table rebuild) could leave a
  half-applied schema. Low risk now (v5 already applied) but relevant before
  adding migration 6+.
- `isUniqueViolation` matches only `"unique"` (not the generic `"constraint"`), so
  NOT NULL / FK / CHECK errors surface as real errors instead of being swallowed
  as dedup conflicts.

## HTTP server

- `WriteTimeout=0` so synchronous long routes aren't cut off; fast routes get a
  15s `http.TimeoutHandler`, `/diagnostics/smtp` 30s, `/send` + trigger none.
  `ReadHeaderTimeout=10s` guards slow clients.
- Auth is a single bearer-token middleware (token from `API_TOKEN`). `/health` is
  unauthenticated.

## Operational history / decisions

- Secrets were migrated from plaintext `config.yaml` into the alice/AnB vault;
  the server is launched via `alice exec` injecting `API_TOKEN`,
  `TELEGRAM_BOT_TOKEN`, `SMTP_PASS`. config.yaml keeps those fields blank.
- The compiled `kind-reminder` binary and runtime files (`*.log`, `*.pid`,
  `config.yaml`, `data/`) are gitignored; don't commit them.
