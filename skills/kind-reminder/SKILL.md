---
name: kind-reminder
description: >-
  Operate, manage, and troubleshoot the self-hosted kind-reminder cron /
  notification service — a Go HTTP server at /Users/bbwave03/claude/kind-reminder
  listening on localhost:8080, with jobs made of ordered shell/webhook/notification
  steps, a SQLite store at data/reminder.db, and Telegram/email/webhook notifiers.
  Use whenever the user wants to restart or upgrade the service (restart.sh),
  trigger or inspect a job (e.g. "Radar 6h report" / radar 日报, backup/encrypt
  jobs, daily reports, ETH brief), run an ad-hoc /send, list/check executions or
  read its logs, create/edit/debug a job or its steps, edit config.yaml, diagnose
  a failing notification or SMTP, or work with its alice/AnB-vaulted secrets
  (kr-api-token / kr-tg-bot-token / kr-smtp-pass). Trigger even on terse asks like
  "restart kind-reminder", "trigger the radar report", "why didn't my reminder
  fire", "看下 kind-reminder 的执行记录", or just naming a job in this service —
  don't make the user spell out the path or the API details.
---

# kind-reminder

A self-hosted cron + notification service (Go module `crontab-reminder`). It runs
**jobs** on cron schedules; each job is an ordered list of **steps** of type
`shell`, `webhook`, or `notification`. State lives in SQLite; notifications go out
via Telegram, SMTP email, or webhook.

- **Repo / working dir:** `/Users/bbwave03/claude/kind-reminder`
- **Server:** local HTTP API on `http://localhost:8080`
- **DB:** `data/reminder.db` (SQLite, WAL) — inspect with the `sqlite3` CLI
- **Logs:** `kind-reminder.log` (JSON lines) · **pidfile:** `kind-reminder.pid`
- **Binary:** `./kind-reminder` (gitignored; built by `restart.sh`)

Always run commands from the repo dir. Read `references/api.md` for the full
endpoint + payload reference; read `references/internals.md` for architecture and
behavioral details when debugging *why* something happened.

## Secrets live in the alice/AnB vault — never on disk, never in your context

The three real secrets are vaulted (see the `anb-secrets` skill for the vault
model). `config.yaml` keeps these fields **blank**; the server gets them from env
at launch (env overrides YAML):

| vault key | env var | what |
|---|---|---|
| `kr-api-token` | `API_TOKEN` | API bearer token (gates the whole API) |
| `kr-tg-bot-token` | `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `kr-smtp-pass` | `SMTP_PASS` | Gmail SMTP app password |

The server is launched through `alice exec`, which resolves
`<agent-vault:KEY>` placeholders into the child's environment. The binary's
absolute path must be blessed in `~/.anb/alice/exec-allowlist.rules` (operator/TTY
task — do not edit that `~/.` file yourself unless the user explicitly authorizes
it). If `alice exec` is denied, it prints the exact rule to add; surface that to
the user, don't retry.

## Restart / upgrade the service (launchd-managed)

The service runs as a **launchd LaunchAgent** `com.bbwave.kind-reminder`
(`~/Library/LaunchAgents/`, repo copy `deploy/`). launchd owns the lifecycle:
`RunAtLoad` + `KeepAlive` (auto-restart on crash; survives terminal/session
close). The plist launches via `alice exec`, so the AnB **`bob` daemon must be
running + unlocked** — if it isn't, the service crash-loops every 10s until it is.

Restart / upgrade is just:

```bash
cd /Users/bbwave03/claude/kind-reminder && ./restart.sh
```

`restart.sh` now does `go build` + `launchctl kickstart -k gui/$(id -u)/com.bbwave.kind-reminder`
(it does NOT hand-launch the binary). Manage it directly with:

```bash
launchctl print    gui/$(id -u)/com.bbwave.kind-reminder   # status / pid / state
launchctl kickstart -k gui/$(id -u)/com.bbwave.kind-reminder  # restart
launchctl bootout  gui/$(id -u)/com.bbwave.kind-reminder    # STOP (a plain kill just relaunches)
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.bbwave.kind-reminder.plist  # (re)load
```

To stop it you must `bootout` — `kill`ing the PID just makes launchd relaunch it.
Never start the binary by hand; it races the managed instance on :8080. The exec
allowlist must bless the binary's absolute path (operator-edited
`~/.anb/alice/exec-allowlist.rules`).

## Authenticated API calls without exposing the token

The API needs `Authorization: Bearer <token>`. Get it from the vault into a curl
config file via `alice write` (resolves the placeholder), use it, delete it — the
token never enters your context or a persistent file. This is the standard pattern
for **every** authenticated call:

```bash
CONF=$(mktemp /tmp/kr-auth.XXXXXX); trap 'rm -f "$CONF"' EXIT
printf 'header = "Authorization: Bearer <agent-vault:kr-api-token>"\n' | alice write "$CONF" >/dev/null
chmod 600 "$CONF"
curl -sS -K "$CONF" http://localhost:8080/jobs | jq .
```

`/health` needs no auth. Everything else does.

## Common operations

Reuse the `$CONF` auth file from above for each authenticated call.

**List jobs (id / title / cron):**
```bash
curl -sS -K "$CONF" http://localhost:8080/jobs \
  | jq -r '.[] | "\(.id)\t\(.title)\t\(.cron_expr)\tenabled=\(.enabled)"'
```

**Trigger a job now** (`?wait=true` blocks for the result; pick a timeout ≥ the
job's real runtime — there is no global write deadline on this route):
```bash
curl -sS -K "$CONF" -X POST \
  "http://localhost:8080/jobs/<JOB_ID>/trigger?wait=true&timeout=60"
```
Without `wait`, it returns an `execution_id` immediately and runs in the
background. To inspect per-step results afterwards:
```bash
curl -sS -K "$CONF" http://localhost:8080/executions/<EXEC_ID> \
  | jq '{status, steps:[.steps[]|{step_id,type,status,exit_code,stdout:(.stdout[0:160])}]}'
```
A healthy notification step shows stdout like `1/1 channels sent`.

**Ad-hoc run without a job** (`POST /send`, synchronous, returns step results —
the AI-agent/CI entry point) and other endpoints: see `references/api.md`.

**Inspect the DB directly** (read-only is safe; the server holds a single writer):
```bash
sqlite3 data/reminder.db "SELECT id,title,cron_expr,enabled,next_run_at FROM jobs;"
sqlite3 data/reminder.db "SELECT version FROM schema_migrations ORDER BY version;"
```

**Tail logs:** `tail -n 50 kind-reminder.log` (JSON lines: `job triggered`,
`execution started/success/failed`, `step attempt`).

## config.yaml — a custom parser, NOT real YAML

`config.yaml` (gitignored, secrets blanked) is read by a hand-rolled parser. Two
sharp edges that will silently corrupt config if ignored:

- **No inline comments.** `workers: 10  # max` makes the value the literal
  `10  # max`. Put comments on their own line only. (`config.yaml.example`
  documents this.)
- **The `queue:` block owns worker/queue settings**, not `scheduler:`. Keys:
  `queue.{type,workers,size,rate_limit_per_sec}` + `scheduler.max_lateness`.
  Legacy `scheduler.workers` / `scheduler.queue_size` are ignored. `type` only
  supports `memory` (redis is a stub). `rate_limit_per_sec` is a **global**
  dispatch cap (0 = unlimited).

Block-sequence lists are supported (surface on `Config.Lists`). Env vars override
YAML — see `references/api.md` for the full env list.

## Troubleshooting quick map

- **Server down / `/health` no response:** check pidfile + port
  (`lsof -nP -iTCP:8080 -sTCP:LISTEN`); read the tail of `kind-reminder.log`; if it
  exited at startup look for a JSON `level:ERROR` line (config load / store open /
  bind). Restart with `restart.sh` (probe first).
- **`alice exec` denied:** binary path not blessed — give the user the rule alice
  prints; don't edit `~/.anb/...` yourself.
- **Notification didn't arrive:** trigger the job and read the execution's
  `notify` step — its `error` aggregates every channel failure
  (`send to "X": ...`). For email, use `POST /diagnostics/smtp` to see which SMTP
  stage fails. Telegram/SMTP creds come from the vault, so a restart that didn't
  go through `alice exec` would leave them empty.
- **A job ran late and got skipped:** that's the no-catchup policy
  (`scheduler.max_lateness`, default 1m) — see `references/internals.md`.
- **Stale `running` executions after a crash/restart:** the server marks them
  `failed` on startup (recovery), and the partial unique index prevents duplicate
  concurrent runs of the same job.
