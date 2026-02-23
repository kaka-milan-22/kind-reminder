# Kind Reminder

**可被 AI / CI / 脚本调用的轻量自动化编排与通知基础设施**

---

## 项目定位

Kind Reminder 是一个 **API-first 的任务调度与自动化执行服务**：

- 基于 **Go + SQLite**，部署简单，无外部依赖
- 支持 **Cron 定时触发**，精确到分钟级
- Job 由多个有序 **Steps** 组成，步骤结果可被后续步骤引用
- 支持三种 Executor：**Notification（通知）/ Webhook / Shell**
- 支持 **Template 模板渲染**，step 输出可注入消息和 webhook body
- 通知渠道支持 **Telegram / Email / Webhook**
- **Docker Compose** 一键部署

本质上是单机版轻量自动化平台（线性 DAG + execution history + context passing + pluggable executors）。

---

## 系统架构

### 资源关系

```
provider        = 发送账号/能力（SMTP / Telegram bot / webhook host）
channel         = 收件目标 + 绑定 provider
job             = 定时规则 + 有序 steps 列表
step            = 单个执行单元（notification / webhook / shell）
execution       = 一次 job 触发记录
execution_step  = 每个 step 的执行结果
```

### 执行路径

```
scheduler tick
  → job due
    → skip if already running（查 DB，非锁）
      → create execution
        → load job_steps ORDER BY order_index
          → for each step:
              executor.Execute(ctx, step)
              → save execution_step
              → ctx.StepResults[step_id] = result
              → if step failed AND NOT continue_on_error: stop
```

### 数据流

```
job → steps → executor → RunContext → template → notification / webhook body
                              ↑
                     StepResults 跨步骤传递
```

---

## 数据模型

### `jobs` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT PK | |
| cron_expr | TEXT | Cron 表达式 |
| timezone | TEXT | 如 Asia/Kuala_Lumpur |
| title | TEXT | job 标题 |
| enabled | INTEGER | 是否启用 |
| next_run_at | TIMESTAMP | 下次执行时间 |
| last_run_at | TIMESTAMP | 上次执行时间 |
| created_at / updated_at | TIMESTAMP | |

> 无 `message`、`channels_json` 字段，由 steps 完全取代。

### `job_steps` 表（新增）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT PK | |
| job_id | TEXT FK | 关联 job，CASCADE DELETE |
| step_id | TEXT | 业务标识符（如 "backup", "notify"） |
| order_index | INTEGER | 执行顺序 |
| type | TEXT | notification / webhook / shell |
| config_json | TEXT | 各类型专属配置 |
| continue_on_error | INTEGER | 默认 0；为 1 时此步失败不中断后续 |
| timeout | INTEGER | 超时秒数 |
| retry | INTEGER | 最多执行次数（含首次，仅 shell 生效） |

**约束：** `UNIQUE(job_id, step_id)`，防止模板引用歧义。

### `executions` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT PK | |
| job_id | TEXT FK | |
| scheduled_at | TIMESTAMP | |
| started_at / finished_at | TIMESTAMP | |
| status | TEXT | pending / running / success / failed |
| error | TEXT | 失败原因 |

### `execution_steps` 表（新增）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT PK | |
| execution_id | TEXT FK | CASCADE DELETE |
| step_id | TEXT | 冗余存储，历史可读 |
| type | TEXT | 冗余存储，历史可读 |
| status | TEXT | success / failed |
| started_at / finished_at | TIMESTAMP | |
| exit_code | INTEGER | shell 用 |
| stdout | TEXT | 截断至 **8KB**，完整日志见文件 |
| stderr | TEXT | 截断至 **8KB**，完整日志见文件 |
| error | TEXT | 模板渲染错误等系统级错误 |
| log_path | TEXT | 完整日志文件路径 |

**日志分层：**
- DB 存截断后的前 8KB，供快速查询
- 完整 stdout/stderr 写文件：`/app/data/executions/<execution_id>/<step_id>.log`

---

## Go 核心抽象

### 数据结构

```go
type StepStatus string

const (
    StepSuccess StepStatus = "success"
    StepFailed  StepStatus = "failed"
)

type Step struct {
    StepID          string
    Type            string
    Config          json.RawMessage
    ContinueOnError bool
    Timeout         int  // 秒
    Retry           int  // 含首次，仅 shell 生效
}

type StepResult struct {
    Status   StepStatus  // 唯一判断依据，不靠 Error 字符串
    ExitCode int
    Stdout   string
    Stderr   string
    Error    string      // 系统级错误（模板渲染失败等）
}

type RunContext struct {
    ExecutionID string
    JobID       string
    Job         *model.Job
    StepResults map[string]StepResult  // step_id → result，跨步骤数据桥
}
```

### Executor 接口

```go
type Executor interface {
    Execute(ctx context.Context, runCtx *RunContext, step Step) StepResult
}
```

### Executor 初始化

```go
NewNotificationExecutor(store *store.Store, reg notifier.Registry)
NewWebhookExecutor()
NewShellExecutor()
```

---

## 三个 Executor 规格

### Notification Executor

```json
{
  "channels": ["tg_ops", "mail_ops"],
  "message_template": "备份完成\nexit={{(index .steps \"backup\").ExitCode}}\n{{(index .steps \"backup\").Stdout}}"
}
```

- 从 DB 查询 channel / provider
- 渲染 message_template 后调用 notifier 发送
- 渲染失败 → Status=failed，写 Error 字段
- 不做 retry（TODO）

### Webhook Executor

```json
{
  "method": "POST",
  "url": "https://example.com/hook",
  "headers": {"X-Token": "abc"},
  "body_template": "{\"exit\": {{(index .steps \"backup\").ExitCode}}}"
}
```

- `body_template` 统一走模板渲染层（与 notification 一致）
- 默认 timeout **10s**（可被 step.timeout 覆盖）
- HTTP status >= 400 → Status=failed
- 记录 status code + response body（截断 8KB）
- 不做 retry（TODO）

### Shell Executor

```json
{
  "script": "/app/scripts/backup.sh"
}
```

- 执行容器内绝对路径脚本（需挂载 `./scripts:/app/scripts`）
- capture stdout / stderr，写完整日志文件，DB 存截断版
- exit code != 0 → Status=failed，触发 retry
- retry：最多 **3 次含首次**，间隔 **5s**
- 支持 timeout context kill

> **TODO**: 目录限制、非 root 用户隔离等安全策略暂不实现

---

## 模板渲染

使用 `text/template`，上下文固定为：

```go
map[string]any{
    "job":   ctx.Job,
    "steps": ctx.StepResults,   // map[string]StepResult
    "now":   time.Now(),
}
```

示例：

```
备份完成 exit={{(index .steps "backup").ExitCode}}
输出：{{(index .steps "backup").Stdout}}
```

渲染失败（step_id 拼错、语法错误）→ 写入 `execution_step.error`，不 panic。

---

## Runner 逻辑（伪代码）

```
// 并发保护：非锁，查 DB
running = COUNT(executions WHERE job_id=? AND status='running')
if running > 0:
    skip, log "job already running"
    return

create execution (pending)
mark execution running

load job_steps ORDER BY order_index

for each step:
    res = executor.Execute(ctx, runCtx, step)
    write stdout/stderr to /app/data/executions/<exec_id>/<step_id>.log
    save execution_step(res, stdout[:8KB], stderr[:8KB], log_path)
    runCtx.StepResults[step.step_id] = res

    if res.Status == failed AND NOT step.ContinueOnError:
        mark execution failed(res.Error)
        return

mark execution success
```

**失败判断统一标准（以 StepResult.Status 为准）：**

| Executor | 触发 failed 的条件 |
|----------|-------------------|
| Shell | ExitCode != 0 |
| Webhook | HTTP status >= 400 |
| Notification | 渲染失败 or 发送失败 |

---

## 并发策略

- Scheduler 维持现有 **worker pool（默认 10）**，jobs 间并发执行
- 同一 job 内 steps **串行**执行，保证顺序和数据传递
- **skip_if_running**：触发前查 DB，已有 running execution 则跳过
  - 实现：`SELECT COUNT(*) FROM executions WHERE job_id=? AND status='running'`
  - SQLite WAL 模式下无需加锁
- **配置建议**（文档约定）：所有 steps 的 timeout 之和应小于 cron 间隔，避免堆积

---

## API

### 端点列表

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /jobs | 创建 job（steps 模型） |
| GET | /jobs | 列出所有 jobs |
| GET | /jobs/:id | 获取单个 job |
| DELETE | /jobs/:id | 删除 job（CASCADE 删除 steps） |
| POST | /providers | 创建 provider |
| GET | /providers | 列出 providers |
| DELETE | /providers/:id | 删除 provider |
| POST | /channels | 创建 channel |
| GET | /channels | 列出 channels |
| DELETE | /channels/:id | 删除 channel |
| GET | /executions | 列出执行历史（含 steps 详情） |
| GET | /stats | 系统统计 |

### POST /jobs 请求示例

```json
{
  "cron": "0 2 * * *",
  "timezone": "Asia/Kuala_Lumpur",
  "title": "每日备份并通知",
  "enabled": true,
  "steps": [
    {
      "step_id": "backup",
      "order_index": 1,
      "type": "shell",
      "timeout": 30,
      "retry": 3,
      "continue_on_error": false,
      "config": {
        "script": "/app/scripts/backup.sh"
      }
    },
    {
      "step_id": "notify",
      "order_index": 2,
      "type": "notification",
      "timeout": 10,
      "continue_on_error": false,
      "config": {
        "channels": ["tg_ops"],
        "message_template": "备份完成\nexit={{(index .steps \"backup\").ExitCode}}\n{{(index .steps \"backup\").Stdout}}"
      }
    }
  ]
}
```

### GET /executions 返回示例

```json
[
  {
    "id": "exec-uuid",
    "job_id": "job-uuid",
    "status": "success",
    "started_at": "2026-02-21T02:00:01Z",
    "finished_at": "2026-02-21T02:00:03Z",
    "steps": [
      {
        "step_id": "backup",
        "type": "shell",
        "status": "success",
        "exit_code": 0,
        "started_at": "2026-02-21T02:00:01Z",
        "finished_at": "2026-02-21T02:00:02Z",
        "duration": "1.2s",
        "stdout": "Backed up 42MB...",
        "log_path": "/app/data/executions/exec-uuid/backup.log"
      },
      {
        "step_id": "notify",
        "type": "notification",
        "status": "success",
        "started_at": "2026-02-21T02:00:02Z",
        "finished_at": "2026-02-21T02:00:03Z",
        "duration": "0.3s"
      }
    ]
  }
]
```

---

## Docker 部署

### docker-compose.yml

```yaml
services:
  app:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data        # SQLite 数据库 + execution 日志
      - ./scripts:/app/scripts  # shell executor 脚本目录
    env_file:
      - .env
```

### 目录结构

```
./data/
  reminder.db
  executions/
    <execution_id>/
      <step_id>.log             # 完整 stdout/stderr

./scripts/
  backup.sh                     # 容器内路径: /app/scripts/backup.sh
```

### 重置系统

```bash
docker compose down
rm -rf data/
docker compose up -d
```

---

## 实现计划

| 步骤 | 内容 | 关键点 |
|------|------|--------|
| 1 | DB 迁移 | 新增 job_steps / execution_steps，清理 jobs 旧字段，UNIQUE 约束 |
| 2 | Go 结构定义 | Step / StepResult(含Status) / RunContext / Executor 接口 |
| 3 | Runner 实现 | handleTask 改写，skip_if_running，串行步骤，continue_on_error |
| 4 | Shell Executor | 路径执行，日志分层存储，retry 5s 间隔 |
| 5 | Webhook Executor | http.Client，10s 默认 timeout，body_template 渲染 |
| 6 | Notification Executor | 复用 notifier，message_template 渲染，channel 从 DB 查 |
| 7 | 模板渲染层 | text/template，注入 job/steps/now，错误写 execution_step.error |
| 8 | API 扩展 | POST /jobs 新模型，GET /executions 含 steps |
| 9 | 端到端验证 | 三条测试路径（见下） |

### 最小验证路径

1. **单 notification** — job 只有一个 notification step，正常发送
2. **shell → notification** — shell 跑完，notification 引用 stdout 发送
3. **webhook only** — webhook step 带 body_template，记录响应

---

## 已知 Risks 与处理

| Risk | 处理方式 |
|------|---------|
| shell exit_code!=0 被误判为成功 | StepResult.Status 统一判断，不靠 Error 字符串 |
| step_id 重复导致模板引用歧义 | UNIQUE(job_id, step_id) 约束 |
| stdout 无限制撑爆 SQLite | DB 截断至 8KB，完整日志写文件 |
| 同 job 并发触发 | skip_if_running 查 DB，有 running 则跳过 |
| 模板 step_id 拼错 | 渲染错误写 execution_step.error，不 panic |
| webhook 超时卡住 | 默认 10s timeout，context cancel |
| shell 脚本不存在 | exit code != 0，retry 3次，最终 failed |
| retry 对 webhook/notification 无效 | 字段保留，逻辑 TODO，代码注释标注 |

---

## 未来增强（已规划）

- Shell 安全策略：目录限制、非 root 用户隔离
- Webhook / Notification retry
- Provider 成功率统计
- `/send` idempotency key（AI 调用友好）
- MQ 支持（Redis / RabbitMQ）
- API key 多租户
