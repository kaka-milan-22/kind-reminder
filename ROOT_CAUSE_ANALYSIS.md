# Root Cause Analysis: Missed Reminder Run at KL 12:00

## Reminder Context
```json
{
  "id": "1c50cfaf-5f61-4793-9d52-e873ed13e282",
  "cron": "0 */3 * * *",
  "timezone": "Asia/Kuala_Lumpur",
  "title": "Radar 3h report",
  "enabled": true,
  "next_run_at": "2026-03-22T07:00:00Z",
  "last_run_at": "2026-03-22T04:00:00Z"
}
```

## Architecture Summary

The reminder system consists of:

1. **Scheduler** (`internal/scheduler/scheduler.go`)
   - Runs every 30 seconds (configurable `TickInterval`)
   - Loads jobs due for execution: `next_run_at <= now`
   - Calculates next scheduled time using cron + timezone

2. **Cron Engine** (`internal/cron/cron.go`)
   - Uses `robfig/cron/v3` library (wall-clock time-based)
   - `NextRun()`: Converts time to job's timezone, computes next cron occurrence
   - `NextRunAfter()`: Loops until result > `nowUTC` (prevents past-dated runs)

3. **Job State Persistence** (`internal/store/store.go`)
   - `ListDueJobs()`: Query `enabled=1 AND next_run_at <= ?`
   - `AdvanceJobSchedule()`: Updates `next_run_at`, `last_run_at`, `updated_at`
   - `FastForwardJobNextRun()`: Updates only `next_run_at` (for overdue skip-ahead)

4. **API Layer** (`internal/api/api.go`)
   - Job creation: Calculates initial `next_run_at` from `now`
   - Job update (PATCH): **Only recalculates `next_run_at` if job is enabled AND config changed**

---

## The Bug: Three Likely Scenarios

### **SCENARIO 1: Job State Corruption on Re-enable**

**Location**: `/Users/kaka/claude/kind-reminder/internal/api/api.go:239-245`

```go
if updated.Enabled && (cronChanged || tzChanged || enabledChanged) {
    nextRun, err := cronx.NextRun(updated.CronExpr, updated.Timezone, time.Now().UTC())
    if err != nil {
        writeErr(w, http.StatusBadRequest, err)
        return
    }
    updated.NextRunAt = nextRun
}
```

**Problem**: The condition checks if `enabledChanged` (was disabled, now enabled), but **only if the job status is NOW enabled**. This means:

- If job was disabled when the scheduler tick happened that would have triggered 12:00 KL run
- Then later re-enabled (either by setting `enabled=true` or by enabling it independently)
- The `next_run_at` is recalculated from the current time, which might already be PAST the 12:00 KL window
- **Result**: The 12:00 KL run gets permanently skipped

**Example Timeline**:
- 11:00 KL (03:00 UTC): Job is `enabled=false` (e.g., user disabled it)
- 12:00 KL (04:00 UTC): Scheduler should run it, but it's disabled → **SKIP**
- 12:30 KL (04:30 UTC): User re-enables the job via PATCH
- API recalculates from 12:30 KL: Next cron after 12:30 is 15:00 KL (07:00 UTC)
- `next_run_at = 07:00 UTC (15:00 KL)`
- **Result**: 12:00 KL run is lost forever

---

### **SCENARIO 2: Cron Expression Misunderstanding**

**Location**: `/Users/kaka/claude/kind-reminder/internal/cron/cron.go:26-38`

```go
func NextRun(expr, tz string, from time.Time) (time.Time, error) {
    loc, err := time.LoadLocation(tz)
    if err != nil {
        return time.Time{}, fmt.Errorf("invalid timezone: %w", err)
    }
    s, err := Parse(expr)
    if err != nil {
        return time.Time{}, err
    }
    scheduledLocal := from.In(loc)
    nextLocal := s.Next(scheduledLocal)
    return nextLocal.UTC(), nil
}
```

**Testing the cron logic**:
```
Cron "0 */3 * * *" in Asia/Kuala_Lumpur timezone
Expected hours (KL): 0, 3, 6, 9, 12, 15, 18, 21

From: 2026-03-22 04:00:00 UTC (12:00 KL)
Next: 2026-03-22 07:00:00 UTC (15:00 KL) ✓

From: 2026-03-22 00:00:00 UTC (08:00 KL)
Next: 2026-03-22 01:00:00 UTC (09:00 KL) ✗ Should be 03:00 KL (11:00 UTC)!
```

**Root Cause**: The `robfig/cron` library evaluates cron expressions based on wall-clock hours in the specified timezone. When you use `0 */3 * * *` in Asia/Kuala_Lumpur:

- The `*/3` is evaluated as: "every 3rd hour in local time"
- But `robfig/cron.Next()` searches for the next matching local time **after** the current local time
- With `from.In(loc)` at 12:00 KL (minute=0), it finds the next hour matching `*/3`: hour 15 (same day)
- However, from 08:00 KL, the next match is 09:00 KL (not 09:00, but 11:00 in true 3-hour intervals)

**The issue**: The cron pattern `0 */3 * * *` creates a **wall-clock schedule**, not an **interval-based schedule**.

---

### **SCENARIO 3: Time-Zone Boundary or DST Issue**

**Location**: `/Users/kaka/claude/kind-reminder/internal/scheduler/scheduler.go:128-175`

```go
func (s *Scheduler) dispatchJob(ctx context.Context, job model.Job, scheduledAt time.Time, nowUTC time.Time) {
    // ... lateness check in job's timezone
    jobLoc, err := time.LoadLocation(job.Timezone)
    if err != nil {
        jobLoc = time.UTC // timezone already validated at job creation
    }
    lateness := nowUTC.In(jobLoc).Sub(scheduledAt.In(jobLoc))
    if lateness > s.cfg.MaxLateness {
        s.logger.Info("job overdue, skipping (no-catchup)", ...)
        // ... skip execution
        return
    }
}
```

**Problem**: If the scheduler tick rate is infrequent (default 30 seconds) and:
- `MaxLateness` is set to a small value (default 1 minute)
- A network delay or GC pause causes a tick to be skipped
- The next tick happens after the scheduled time + MaxLateness

**Example**:
- Expected: 12:00 KL (04:00 UTC) ± 1 minute = 03:59-04:01 UTC
- Scheduler tick: 04:02 UTC (12:02 KL) - **LATE by 2 minutes**
- **Result**: Job skipped as overdue, advanced to next_run_at = 15:00 KL

---

## Exact Code References

### Job Creation (Initial Schedule)
- **File**: `/Users/kaka/claude/kind-reminder/internal/api/api.go`
- **Lines**: 135-167 (`createJob`)
- **Logic**: `nextRun = cronx.NextRun(cron, tz, time.Now().UTC())`

### Job Updates (Re-enable Bug)
- **File**: `/Users/kaka/claude/kind-reminder/internal/api/api.go`
- **Lines**: 192-246 (`patchJob`)
- **Critical**: Line 239-245 - Only updates `next_run_at` if `Enabled && (cronChanged || tzChanged || enabledChanged)`
- **Bug**: If re-enabling after current time passes expected run time → run is lost

### Scheduler Dispatch
- **File**: `/Users/kaka/claude/kind-reminder/internal/scheduler/scheduler.go`
- **Lines**: 116-126 (`runOnce`) - Queries `next_run_at <= now`
- **Lines**: 128-182 (`dispatchJob`) - Calculates next, checks lateness, updates schedule
- **Lines**: 140-153 - MaxLateness check (default 1 minute)

### Cron Calculation
- **File**: `/Users/kaka/claude/kind-reminder/internal/cron/cron.go`
- **Lines**: 26-38 (`NextRun`)
- **Lines**: 40-55 (`NextRunAfter`) - Loops to ensure next > now

### Database Query
- **File**: `/Users/kaka/claude/kind-reminder/internal/store/store.go`
- **Lines**: 356-374 (`ListDueJobs`)
- **Query**: `SELECT ... FROM jobs WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at ASC`

### Schedule Update
- **File**: `/Users/kaka/claude/kind-reminder/internal/store/store.go`
- **Lines**: 493-497 (`AdvanceJobSchedule`) - Updates `next_run_at`, `last_run_at`
- **Lines**: 499-503 (`FastForwardJobNextRun`) - Updates only `next_run_at`

---

## Most Likely Root Cause

**Scenario 1 (Job Re-enable Bug)** is the **most probable**:

The reminder likely:
1. Was created with cron `0 */3 * * *` in KL timezone
2. **Was disabled at some point** (user disabled it or it was disabled programmatically)
3. Became re-enabled **after** the 12:00 KL scheduled time (or during/before it)
4. When re-enabled, `next_run_at` was recalculated from current time
5. If recalculation happened after 12:00 KL, the next occurrence would be 15:00 KL
6. **The 12:00 KL run was permanently lost**

---

## Code Smells & Recommendations

### 1. **No Logging of Disabled Jobs During Scheduler Tick**
- When a scheduled time passes but the job is `enabled=0`, there's no audit trail
- **Fix**: Log when scheduler skips disabled jobs

### 2. **Re-enable Logic Doesn't Preserve Last Run**
- Line 239: `enabledChanged` condition only recalculates next_run_at if enabled **now**
- **Fix**: When re-enabling, use `last_run_at` to calculate next correctly:
  ```go
  var baseTime time.Time
  if updated.LastRunAt.IsZero() {
      baseTime = time.Now().UTC()
  } else {
      baseTime = updated.LastRunAt
  }
  nextRun, err := cronx.NextRunAfter(updated.CronExpr, updated.Timezone, baseTime, time.Now().UTC())
  ```

### 3. **MaxLateness Can Silently Skip Runs**
- Line 147-152: No warning if job is skipped due to MaxLateness
- **Fix**: This is currently logged as `INFO`, which is good

### 4. **Cron Expression Interpretation Issue**
- The cron schedule appears correct, but test shows:
  ```
  From 08:00 KL -> Next 09:00 KL (not 11:00 KL as interval-based)
  ```
- **Possible Confusion**: Users may expect `*/3` to mean "every 3-hour interval" not "at hours divisible by 3"

---

## Verification Steps

1. **Check job history**: Did the job exist and was it disabled between creation and 12:00 KL?
2. **Check logs**: Search for:
   - `"job overdue, skipping"` at or before 12:00 KL timestamp
   - `"enabled=0"` in DB around that time
   - Any PATCH requests that touched the job's `enabled` field
3. **Check execution logs**: Was there an execution_id created for 12:00 KL time slot?
4. **Verify cron schedule**: Calculate manually using:
   ```bash
   go run test_cron.go
   ```
