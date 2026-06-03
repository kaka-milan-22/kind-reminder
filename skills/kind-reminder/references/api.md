# kind-reminder API & config reference

All endpoints except `GET /health` require `Authorization: Bearer <token>`.
Use the `alice write` → curl `-K` pattern from SKILL.md so the token never enters
context. Base URL `http://localhost:8080`.

## Endpoints

| Method & path | Purpose | Notes |
|---|---|---|
| `GET /health` | Liveness | No auth; returns `ok` |
| `GET /stats` | Service stats | enabled jobs, last finished, recent failures, scheduler/queue status |
| `POST /jobs` | Create a job | body = job request (below); returns `{id}` |
| `GET /jobs` | List jobs | array, newest first |
| `GET /jobs/{id}` | Get one job (with steps) | 404 if absent |
| `PATCH /jobs/{id}` | Update fields | partial; omit `steps` to keep them, send `steps` to fully replace |
| `DELETE /jobs/{id}` | Delete job | cascades steps/executions |
| `POST /jobs/{id}/trigger` | Run a job now | `?wait=true&timeout=N` blocks for result; `Idempotency-Key` header supported; 409 if already running |
| `GET /executions` | List executions | `?job_id=&limit=&offset=&include_adhoc=true` |
| `GET /executions/{id}` | One execution + steps | per-step status/stdout/stderr/exit_code |
| `POST /send` | Ad-hoc run, no job | synchronous, returns step results; AI-agent/CI entry |
| `POST /providers` `GET /providers` `DELETE /providers/{id}` | Notifier providers | bound tokens/SMTP creds for channels |
| `POST /channels` `GET /channels` `DELETE /channels/{id}` | Named channels | referenced by notification steps |
| `POST /diagnostics/smtp` | SMTP probe | DNS→TCP→greeting→EHLO→STARTTLS→AUTH→MAIL→RCPT staged result |

Route timeouts: fast routes are wrapped in a 15s `http.TimeoutHandler`,
`/diagnostics/smtp` in 30s; `/send` and `/jobs/{id}/trigger` have **no** blanket
timeout (server `WriteTimeout=0`) so long synchronous work isn't cut off.

## Job request (POST /jobs, PATCH /jobs/{id})

```json
{
  "cron": "0 */6 * * *",
  "timezone": "Asia/Kuala_Lumpur",
  "title": "Radar 6h report",
  "enabled": true,
  "steps": [
    {
      "step_id": "radar",
      "order_index": 1,
      "type": "shell",
      "config": { "script": "/path/to/radar.sh" },
      "timeout": 120,
      "retry": 1,
      "continue_on_error": false
    },
    {
      "step_id": "notify",
      "order_index": 2,
      "type": "notification",
      "config": {
        "channels": ["tg_ops"],
        "title_template": "📡 Radar {{now}}",
        "message_template": "{{ (index .steps \"radar\").Stdout }}"
      }
    }
  ]
}
```

Step fields: `step_id` (required, unique within job), `order_index` (asc),
`type` (`shell`|`webhook`|`notification`), `config` (type-specific, below),
`timeout` (seconds, default 300, enforced via ctx), `retry` (max attempts,
default 1 = no retry; backoff 2s→5s→10s), `continue_on_error` (default true).

### Step config by type

**shell** — runs the whole string through `/bin/sh -c` (full shell syntax). Output
is captured (capped 64KB in memory, persisted 8KB).
```json
{ "script": "echo hi && /usr/local/bin/radar.sh" }
```

**webhook** — request bounded by the step's ctx (no fixed client timeout).
```json
{ "method": "POST", "url": "https://...", "headers": {"X-Foo":"bar"},
  "body_template": "{\"text\":\"{{ ... }}\"}" }
```

**notification** — sends to one or more named channels; errors across channels are
aggregated (every failure reported), stdout summarizes `N/M channels sent`.
```json
{ "channels": ["tg_ops","mail_ops"], "title_template": "...", "message_template": "..." }
```

Templates are Go `text/template` with `.job`, `.steps` (map keyed by step_id →
StepResult with `.Stdout/.Stderr/.ExitCode/.Status`), and `.now`.

## POST /send (ad-hoc)

```json
{ "timezone": "Asia/Shanghai",
  "steps": [ { "step_id":"alert","order_index":1,"type":"notification",
               "config":{"channels":["tg_ops"],"message_template":"done 🚀"} } ] }
```
Returns `{execution_id, status, steps:[...]}` synchronously.

## Environment variables (override config.yaml)

`CONFIG_FILE` (path, default `./config.yaml`), `SERVER_PORT`, `DB_PATH`,
`API_TOKEN`, `TELEGRAM_BOT_TOKEN`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`,
`SMTP_PASS`, `SMTP_FROM`, `WORKERS`, `QUEUE_SIZE`, `QUEUE_TYPE`, `QUEUE_WORKERS`,
`RATE_LIMIT_PER_SEC`, `WEBHOOK_BASE_URL`, `WEBHOOK_TIMEOUT_SECONDS`,
`WEBHOOK_ENABLED`. The three secret-bearing ones (`API_TOKEN`,
`TELEGRAM_BOT_TOKEN`, `SMTP_PASS`) are injected by `alice exec` at launch.
