# Kind Reminder

**单机自动化任务编排引擎。** 定时执行一组有序 Steps（Shell → Webhook → Notification），步骤结果可被后续步骤引用。API-first，无外部依赖。

---

## Quick Start

```bash
cp config.yaml.example config.yaml
# 至少设置 api_token
go run ./cmd/server
curl http://localhost:8080/health  # → ok
```

Docker：

```bash
mkdir -p data scripts
docker compose up -d --build
```

---

## 核心概念

```
Provider  = 发送凭证（Telegram bot token / SMTP / webhook host）
Channel   = 收件目标（chat_id / email / URL）+ 绑定 Provider
Job       = Cron 调度规则 + 有序 Steps
Step      = 单个执行单元（shell / webhook / notification）
Execution = 一次 Job 触发的完整运行记录
```

**执行模型**：每个 Step 串行执行，前一 Step 的输出可在后续 Step 的模板中引用。

---

## Step 类型

每个 Step 都包含以下通用字段：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `step_id` | string | ✅ | — | Step 唯一标识，后续 Step 可通过 `(index .steps "step_id")` 引用本 Step 的结果 |
| `order_index` | int | ❌ | 0 | 执行顺序，从小到大依次执行；相同值按提交顺序执行 |
| `type` | string | ✅ | — | Step 类型：`shell` / `webhook` / `notification` |
| `timeout` | int | ❌ | 300 | 超时秒数，超时后强制终止该 Step |
| `retry` | int | ❌ | 1 | 最大执行次数（1 = 不重试），失败后按 2s → 5s → 10s 退避重试 |
| `continue_on_error` | bool | ❌ | true | 该 Step 失败后是否继续执行后续 Step |
| `config` | object | ✅ | — | Step 具体配置，内容因 `type` 而不同，见下方各类型说明 |

---

### shell

执行容器内脚本文件，适合备份、清理、数据处理等任务。

**config 字段：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `script` | string | ✅ | 脚本文件的绝对路径（容器内路径），直接作为可执行文件调用，不经过 shell 解释器 |

```json
{
  "step_id": "backup",
  "order_index": 1,
  "type": "shell",
  "timeout": 60,
  "retry": 3,
  "config": {
    "script": "/app/scripts/backup.sh"
  }
}
```

> - `exit_code != 0` → failed，`exit_code` 可在后续 Step 模板中通过 `(index .steps "backup").ExitCode` 引用
> - `stdout` / `stderr` 同样可在模板中引用，内容截断上限 8KB

---

### webhook

向外部 HTTP 服务发起请求，`body` 和 `headers` 均支持模板渲染。

**config 字段：**

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `url` | string | ✅ | — | 目标请求地址 |
| `method` | string | ❌ | `POST` | HTTP 方法，支持 `GET` `POST` `PUT` `PATCH` `DELETE` |
| `headers` | object | ❌ | — | 自定义请求头，key/value 均为字符串，value 支持模板渲染 |
| `body_template` | string | ❌ | — | 请求体内容，支持模板渲染；有内容时自动设置 `Content-Type: application/json` |

```json
{
  "step_id": "notify_api",
  "order_index": 2,
  "type": "webhook",
  "timeout": 10,
  "config": {
    "method": "POST",
    "url": "https://example.com/hook",
    "headers": { "X-Token": "abc" },
    "body_template": "{\"exit\": {{(index .steps \"backup\").ExitCode}}}"
  }
}
```

> - HTTP status `>= 400` → failed
> - 响应体（最多 8KB）会记录到该 Step 的 `stdout`，可供后续 Step 引用

---

### notification

通过已配置的 Channel 发送消息，消息标题和正文均支持模板渲染。

**config 字段：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `channels` | array | ✅ | Channel ID 列表（在 `/channels` 接口中创建），多个 Channel 同时发送 |
| `message_template` | string | ✅ | 消息正文，支持模板渲染，可引用前置 Step 结果和 `.now` 时间 |
| `title_template` | string | ❌ | 消息标题，支持模板渲染；不填时使用 Job 标题（adhoc 执行时为空） |

```json
{
  "step_id": "notify",
  "order_index": 3,
  "type": "notification",
  "config": {
    "channels": ["tg_ops"],
    "title_template": "备份报告 {{ .now.Format \"01-02\" }}",
    "message_template": "备份完成 ✅\nexit={{ (index .steps \"backup\").ExitCode }}\n{{ (index .steps \"backup\").Stdout }}"
  }
}
```

> - 任意一个 Channel 发送失败 → 该 Step 整体 failed
> - `title_template` 渲染结果优先于 Job 标题

---

## 模板语法

所有 `*_template` 字段使用 Go `text/template`。

**可用变量：**

| 变量 | 类型 | 说明 |
|------|------|------|
| `.job.ID` | string | Job ID |
| `.job.Title` | string | Job 标题 |
| `.steps` | map | 已完成 Steps 的结果 |
| `.now` | time.Time | 当前时间 |

**StepResult 字段：**

| 字段 | 说明 |
|------|------|
| `.Status` | `"success"` 或 `"failed"` |
| `.ExitCode` | exit code（shell 用） |
| `.Stdout` | 标准输出（截断 8KB） |
| `.Stderr` | 标准错误（截断 8KB） |
| `.Error` | 系统级错误信息 |

**示例：**

```
backup exit={{ (index .steps "backup").ExitCode }}
stdout={{ (index .steps "backup").Stdout }}
time={{ .now.Format "2006-01-02 15:04:05" }}
```

渲染失败 → step status=failed，写入 error 字段，不 panic。

---

## API

所有端点（除 `/health`）需要：

```
Authorization: Bearer <api_token>
```

### Providers

```bash
# 创建 Telegram provider
curl -X POST http://localhost:8080/providers \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{"id":"bot_ops","type":"telegram","config":{"bot_token":"111:AAA..."}}'

# 创建 Email provider
curl -X POST http://localhost:8080/providers \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{"id":"smtp_ops","type":"email","config":{"host":"smtp.gmail.com","port":587,"user":"x@g.com","pass":"xxx","from":"x@g.com"}}'

# 列出
curl http://localhost:8080/providers -H "Authorization: Bearer xxx"

# 删除
curl -X DELETE http://localhost:8080/providers/bot_ops -H "Authorization: Bearer xxx"
```

### Channels

```bash
# Telegram channel
curl -X POST http://localhost:8080/channels \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{"id":"tg_ops","type":"telegram","provider_id":"bot_ops","config":{"chat_id":"-1001234567890"}}'

# Email channel
curl -X POST http://localhost:8080/channels \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{"id":"mail_ops","type":"email","provider_id":"smtp_ops","config":{"to":"ops@company.com"}}'

# 列出
curl http://localhost:8080/channels -H "Authorization: Bearer xxx"
```

### Jobs

```bash
# 创建 Job（shell → notification）
curl -X POST http://localhost:8080/jobs \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "cron": "0 2 * * *",
    "timezone": "Asia/Shanghai",
    "title": "每日备份",
    "enabled": true,
    "steps": [
      {
        "step_id": "backup",
        "order_index": 1,
        "type": "shell",
        "timeout": 60,
        "retry": 3,
        "config": { "script": "/app/scripts/backup.sh" }
      },
      {
        "step_id": "notify",
        "order_index": 2,
        "type": "notification",
        "config": {
          "channels": ["tg_ops"],
          "message_template": "备份完成\nexit={{ (index .steps \"backup\").ExitCode }}\n{{ (index .steps \"backup\").Stdout }}"
        }
      }
    ]
  }'

# 列出（仅 metadata）
curl http://localhost:8080/jobs -H "Authorization: Bearer xxx"

# 获取单个（含 steps）
curl http://localhost:8080/jobs/<id> -H "Authorization: Bearer xxx"

# 更新（steps 全量替换）
curl -X PATCH http://localhost:8080/jobs/<id> \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'

# 删除
curl -X DELETE http://localhost:8080/jobs/<id> -H "Authorization: Bearer xxx"
```

**Cron 表达式（5 字段）：**

| 格式 | 含义 |
|------|------|
| `* * * * *` | 每分钟 |
| `0 9 * * 1-5` | 工作日 09:00 |
| `*/15 * * * *` | 每 15 分钟 |
| `0 2 * * *` | 每天 02:00 |

### Executions

```bash
# 列出执行历史（含每步详情）
curl "http://localhost:8080/executions?limit=50&offset=0&job_id=<id>" \
  -H "Authorization: Bearer xxx"

# 包含 /send 的即时执行
curl "http://localhost:8080/executions?include_adhoc=true" \
  -H "Authorization: Bearer xxx"
```

参数：`limit`（默认 50，最大 500）、`offset`、`job_id`、`include_adhoc`

Execution 记录新增字段（v3）：

| 字段 | 说明 |
|------|------|
| `trigger_type` | `cron` / `manual` / `adhoc` |
| `triggered_at` | 实际启动时间（cron 调度延迟 = `triggered_at − scheduled_at`） |
| `triggered_by` | `system`（cron）/ `api`（手动触发） |
| `scheduled_at` | Cron 计划时间（手动触发为 `null`） |

### POST /jobs/{id}/trigger（手动触发）

手动立即执行一个已有 Job，**不影响 Cron 调度**（`next_run_at` 保持不变）。

```bash
# 基本用法
curl -X POST http://localhost:8080/jobs/<job-id>/trigger \
  -H "Authorization: Bearer xxx"

# 同步等待执行完成（默认超时 30s）
curl -X POST "http://localhost:8080/jobs/<job-id>/trigger?wait=true&timeout=60" \
  -H "Authorization: Bearer xxx"

# 携带幂等键（防止重复触发）
curl -X POST http://localhost:8080/jobs/<job-id>/trigger \
  -H "Authorization: Bearer xxx" \
  -H "Idempotency-Key: my-unique-key-001"

# 覆盖 Step 配置参数（调试用）
curl -X POST http://localhost:8080/jobs/<job-id>/trigger \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "override": {
      "backup_step": { "script": "/tmp/test.sh" }
    }
  }'
```

**响应（异步，默认）：** HTTP 201
```json
{ "execution_id": "xxxxx", "status": "running", "trigger": "manual" }
```

**响应（`?wait=true`，执行完成）：** HTTP 200
```json
{ "execution_id": "xxxxx", "status": "success", "trigger": "manual" }
```

**响应（`?wait=true`，超时仍在运行）：** HTTP 202
```json
{ "execution_id": "xxxxx", "status": "running", "trigger": "manual" }
```

**响应（Job 正在运行，拒绝并发）：** HTTP 409
```json
{ "error": "job already running" }
```

| 参数 / Header | 说明 |
|---|---|
| `?wait=true` | 阻塞直到执行完成，最多等待 `timeout` 秒 |
| `?timeout=N` | 与 `wait=true` 配合，等待秒数（默认 30） |
| `Idempotency-Key` | 幂等键，相同 key 重复请求返回已有 execution |
| `override.{step_id}` | 覆盖指定 Step 的 config 字段（仅 config 合并，不可修改 type/order_index/step_id） |

### GET /executions/{id}（查看单次执行详情）

```bash
curl http://localhost:8080/executions/<execution-id> \
  -H "Authorization: Bearer xxx"
```

响应包含完整执行信息及每个 Step 的 stdout/stderr：

```json
{
  "id": "exec-uuid",
  "job_id": "job-uuid",
  "trigger_type": "manual",
  "triggered_by": "api",
  "triggered_at": "2026-03-14T04:00:00Z",
  "scheduled_at": null,
  "status": "success",
  "steps": [
    {
      "step_id": "backup",
      "status": "success",
      "stdout": "...",
      "stderr": "",
      "started_at": "...",
      "finished_at": "..."
    }
  ]
}
```

### POST /send（即时执行）

无需创建 Job，直接提交 Steps 执行。AI Agent / CI 调用入口。

**顶层请求字段：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `steps` | array | ✅ | 要执行的 Step 列表，按 `order_index` 顺序依次执行 |
| `timezone` | string | ❌ | 时区，影响消息模板中时间变量（如 `{{now}}`）的渲染。默认使用系统时区。示例：`"Asia/Shanghai"`、`"Asia/Kuala_Lumpur"`、`"UTC"` |

**steps 子字段（每个 Step）：**

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `step_id` | string | ✅ | — | Step 唯一标识，后续 Step 通过 `(index .steps "step_id")` 引用前置结果 |
| `order_index` | int | ❌ | 0 | 执行顺序（从小到大），相同值按提交顺序执行 |
| `type` | string | ✅ | — | Step 类型：`shell` / `webhook` / `notification` |
| `config` | object | ✅ | — | Step 具体配置，内容因 `type` 而不同（见下方各类型说明） |
| `timeout` | int | ❌ | 300 | 超时秒数，超时后强制终止该 Step |
| `retry` | int | ❌ | 1 | 最大执行次数（1 = 不重试），失败后按 2s → 5s → 10s 退避重试 |
| `continue_on_error` | bool | ❌ | true | 该 Step 失败后是否继续执行后续 Step |

```bash
curl -X POST http://localhost:8080/send \
  -H "Authorization: Bearer xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "timezone": "Asia/Kuala_Lumpur",
    "steps": [
      {
        "step_id": "alert",
        "order_index": 1,
        "type": "notification",
        "config": {
          "channels": ["tg_ops"],
          "message_template": "部署完成 🚀"
        }
      }
    ]
  }'
```

响应：

```json
{
  "execution_id": "uuid",
  "status": "success",
  "steps": [
    { "step_id": "alert", "type": "notification", "status": "success", "..." : "..." }
  ]
}
```

### Stats

```bash
curl http://localhost:8080/stats -H "Authorization: Bearer xxx"
```

---

## 配置

`config.yaml`：

```yaml
server_port: "8080"
db_path: "./reminder.db"
api_token: "your-secret-token"   # 必须设置

telegram:
  bot_token: "<bot-token>"       # 全局默认（channel 无 provider 时用）

smtp:
  host: "smtp.gmail.com"
  port: 587
  user: "you@gmail.com"
  pass: "app-password"
  from: "you@gmail.com"

scheduler:
  workers: 10
  queue_size: 100
  max_lateness: 1m    # 超过此时间的历史任务直接丢弃，不补跑

queue:
  type: "memory"
  workers: 10
  size: 1000
  rate_limit_per_sec: 20

webhook:
  enabled: true
  base_url: "https://your-host.com"
  timeout_seconds: 5
```

环境变量覆盖（优先级高于 yaml）：`API_TOKEN`、`DB_PATH`、`SERVER_PORT`、`TELEGRAM_BOT_TOKEN`、`SMTP_HOST`、`SMTP_PORT`、`SMTP_USER`、`SMTP_PASS`、`SMTP_FROM`

> **Scheduler 调度策略（No-Catchup 模式）**
>
> Kind Reminder 使用标准 cron 语义：**服务停机期间的任务不会在恢复后补跑**。
>
> - 每 30s tick 一次，触发 `next_run_at ≤ now` 的 Job
> - tick 到达时，如果 `now - scheduled_at > max_lateness`（默认 1 分钟），该次执行被丢弃，`next_run_at` 直接推进到下一个未来时间点
> - 服务重启后同样适用：历史 `next_run_at` 自动快进，不触发补偿执行

---

## Docker 部署

```yaml
# docker-compose.yml
services:
  app:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data          # SQLite + 执行日志
      - ./scripts:/app/scripts    # Shell 脚本
    env_file:
      - .env
```

```bash
mkdir -p data scripts
cp config.yaml.example config.yaml
docker compose up -d --build
```

---

## Step 控制字段

| 字段 | 默认值 | 说明 |
|------|-------|------|
| `timeout` | 300s | 超时秒数，超时后强制终止 |
| `retry` | 1 | 最大执行次数（1=不重试）|
| `continue_on_error` | true | 失败后是否继续执行后续 step |

Retry backoff：2s → 5s → 10s（固定）

---

## 系统边界（v2 不支持）

- 分布式调度 / 多节点
- DAG（仅支持线性 steps）
- Shell 沙箱安全隔离
- Webhook / Notification retry（executor 级别）
- MQ 队列后端
