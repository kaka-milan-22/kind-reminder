# Kind Reminder v2

**API-first 自动化调度与执行引擎（单机版）**

Kind Reminder 是一个轻量级任务编排系统：

* Go + SQLite（WAL）
* Cron 调度
* Job = 有序 Steps
* 支持 Shell / Webhook / Notification
* Step 输出可被后续 Step 引用
* 原子并发控制
* Docker 单机部署

---

# 一、核心模型

## 资源关系

```
provider   = 发送能力（SMTP / Telegram / Webhook endpoint）
channel    = 收件目标 + provider
job        = 调度规则 + steps
step       = 单个执行单元
execution  = 一次 job 触发
step_run   = 单个 step 执行结果
```

---

## 执行路径

```
scheduler tick
  → due job
    → INSERT execution(status=running)
      (唯一约束保证同 job 不并发)
        → load steps ordered
          → for step:
              executor.Execute(ctx, step)
              save step_run
              ctx.results[step_id] = result
              if failed AND !continue_on_error:
                   stop
        → update execution final status
```

---

## 数据流

```
job → steps → executor → RunContext → template → notifier/http/shell
                              ↑
                        step results map
```

---

# 二、数据库设计

## jobs

```
id TEXT PK
cron_expr TEXT NOT NULL
timezone TEXT NOT NULL
title TEXT
enabled INTEGER NOT NULL DEFAULT 1
next_run_at TIMESTAMP
last_run_at TIMESTAMP
created_at TIMESTAMP
updated_at TIMESTAMP
```

---

## job_steps

```
id TEXT PK
job_id TEXT NOT NULL
step_id TEXT NOT NULL
order_index INTEGER NOT NULL
type TEXT NOT NULL
config_json TEXT NOT NULL
timeout INTEGER
retry INTEGER DEFAULT 0
continue_on_error INTEGER DEFAULT 0
```

### 必须约束

```
UNIQUE(job_id, step_id)
UNIQUE(job_id, order_index)
FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
```

保证：

* step 可稳定引用
* 执行顺序确定

---

## executions

```
id TEXT PK
job_id TEXT NOT NULL
scheduled_at TIMESTAMP
started_at TIMESTAMP
finished_at TIMESTAMP
status TEXT NOT NULL
error TEXT
```

### 并发控制核心

```sql
CREATE UNIQUE INDEX uniq_running_job
ON executions(job_id)
WHERE status='running' AND job_id != '__adhoc__';
```

插入 running execution 时：

* 成功 → 获得执行权
* 冲突 → 说明已有运行中 → scheduler skip

无需 SELECT，无 TOCTOU。adhoc execution 允许同时运行多个。

---

## execution_steps

```
id TEXT PK
execution_id TEXT NOT NULL
step_id TEXT NOT NULL
type TEXT NOT NULL
status TEXT NOT NULL
started_at TIMESTAMP
finished_at TIMESTAMP
exit_code INTEGER
stdout TEXT
stderr TEXT
error TEXT
```

stdout/stderr：

* 写入 DB 前截断 ≤ 8KB
* 完整日志写：

```
/data/executions/<execution>/<step>.log
```

---

## schema_migrations

```
version INTEGER PRIMARY KEY
applied_at TIMESTAMP
```

启动时自动执行未应用 migration。

---

# 三、Go 抽象

## Step

```go
type Step struct {
    StepID string
    Type   string
    Config json.RawMessage

    Timeout int
    Retry   int
    ContinueOnError bool
}
```

---

## StepResult

```go
type StepResult struct {
    Status   string // success / failed
    ExitCode int
    Stdout   string
    Stderr   string
    Error    string
}
```

不要用 Error 字符串推断状态。

---

## RunContext

```go
type RunContext struct {
    ExecutionID string
    JobID string
    Job *Job

    Results map[string]StepResult
}
```

这是 steps 数据桥。

---

## Executor 接口

```go
type Executor interface {
    Execute(ctx context.Context, runCtx *RunContext, step Step) StepResult
}
```

* `ctx`：生命周期/取消/timeout（技术控制）
* `runCtx`：业务数据桥（step 结果、job 信息）

Runner 调用：

```go
stepCtx, cancel := context.WithTimeout(parentCtx, step.Timeout)
defer cancel()
res := executor.Execute(stepCtx, runCtx, step)
```

---

# 四、Executor 设计

## ShellExecutor

config:

```json
{
  "script": "/app/scripts/backup.sh"
}
```

行为：

* exec.CommandContext
* capture stdout/stderr
* 超时 kill
* exit_code!=0 → failed
* retry 按 step.retry

安全策略后续再加。

---

## WebhookExecutor

config:

```json
{
  "method": "POST",
  "url": "https://example/api",
  "headers": {
    "X-Token": "{{.job.id}}"
  },
  "body_template": "backup={{(index .steps \"backup\").ExitCode}}"
}
```

行为：

* 渲染模板（headers/body）
* 默认 timeout=10s
* 记录 status code + body（截断）
* HTTP >=400 → failed

---

## NotificationExecutor

config:

```json
{
  "channels": ["tg_ops"],
  "message_template": "backup ok {{(index .steps \"backup\").Stdout}}"
}
```

行为：

* 渲染模板
* 查 channel → provider
* 调 notifier 发送
* notifier 内部负责 retry

executor 不实现发送 retry。

---

# 五、模板系统

使用：

```
text/template
```

上下文：

```go
{
  "job":   ctx.Job,
  "steps": ctx.Results,
  "now":   time.Now(),
}
```

规则：

* 模板错误 → step.status=failed
* 写入 execution_steps.error

所有 executor 行为一致。

---

# 六、Runner 逻辑

伪代码：

```
INSERT execution(status=running)
IF conflict:
    return

FOR step ordered:

    attempts = max(1, step.retry)

    FOR i in 1..attempts:

        res = executor.Execute()

        save execution_step

        IF res.status == success:
            break

        IF i < attempts:
            sleep(backoff(i))  // 2s / 5s / 10s

    ctx.results[step_id] = res

    IF res.status=failed AND !continue_on_error:
        mark execution failed
        return

mark execution success
```

Retry 规则：

* retry 在 Runner 层统一实现
* executor 只执行一次
* notifier 内部发送 retry 保留（独立）
* backoff 固定：1st=2s，2nd=5s，3rd=10s

---

# 七、Scheduler

* tick = 30s
* 查询 next_run_at ≤ now（截断到分钟）
* 尝试 INSERT running execution
* 成功才执行

## No-Catchup 模式（标准 cron 语义）

服务停机期间错过的任务**永不补跑**。每次 `dispatchJob` 前：

```
lateness = now.In(jobTZ) - scheduledAt.In(jobTZ)

if lateness > max_lateness (默认 1m):
    FastForwardJobNextRun(cron.Next(now))  ← 一定 > now
    return  ← 不写 execution，不执行任何 step
```

关键：
- `next_run_at` 通过 `NextRunAfter(scheduledAt, now)` 推进——内部循环直到结果严格 `> now`，不可能仍在过去
- lateness 用 **job 本地时区**计算，DST 切换和跨时区部署均安全
- skip 不写 execution 记录，历史数据干净

### 行为示例

```
cron: 0 */2 * * *

12:00 ✔ 执行
13:00 服务停止
14:00 错过 → 丢弃（不执行）
15:30 服务恢复
16:00 ✔ 正常执行
```

## 并发控制

* job 间并发允许
* 同 job 不并发（DB partial unique index 约束）

## 配置

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `scheduler.max_lateness` | `1m` | 超过此时间的历史任务丢弃，不补跑 |
| tick interval | `30s` | 调度轮询间隔（硬编码） |

---

# 八、API

## POST /jobs

创建 job

```
cron
timezone
title
enabled
steps[]
```

---

## GET /jobs

只返回 metadata（不含 steps）

---

## GET /jobs/:id

返回：

```
job + steps[]
```

---

## PATCH /jobs/:id

允许：

* cron/timezone/title/enabled
* steps（全量替换）

---

## DELETE /jobs/:id

删除 job（cascade steps）

---

## GET /executions

```
?limit=50&offset=0
```

返回：

```
execution + steps[]
```

默认 limit=50，最大=500。

---

## POST /send

即时发送（无 job）

```
steps: [ notification | webhook | shell ]
```

内部：

* 创建临时 execution，`job_id="__adhoc__"`
* 执行 steps
* 返回结果

规则：

* `__adhoc__` 为系统保留 job_id
* scheduler 永远忽略该 job_id
* API 查询默认过滤：`WHERE job_id != '__adhoc__'`
* 如需查看：`GET /executions?include_adhoc=true`
* UNIQUE running index 排除 `__adhoc__`，允许同时运行多个

这是 AI / CI 调用入口。

---

# 九、Docker

```
/data/reminder.db
/data/executions/
/scripts/
```

compose:

```yaml
volumes:
  - ./data:/app/data
  - ./scripts:/app/scripts
```

---

# 十、最小实现顺序

1. DB schema + migration
2. execution unique index
3. runner 串行执行链
4. Shell executor
5. Notification executor
6. Webhook executor
7. template 层
8. API
9. scheduler

完成第 4 步即可端到端运行。

---

# 十一、系统边界（明确不做）

v2 不包含：

* 分布式调度
* MQ
* DAG（只支持线性 steps）
* shell 沙箱安全
* webhook retry
* provider metrics

这些留到 v3。

---

# 总结

Kind Reminder v2 的本质：

**单机确定性自动化执行引擎**

关键保证：

* DB 约束实现并发安全
* step 结果可引用
* executor 与 notifier 解耦
* migration 可持续升级

做到这些，它就不是提醒工具，而是可长期演化的自动化基础设施。
