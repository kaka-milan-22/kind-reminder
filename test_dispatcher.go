package main

import (
"fmt"
"time"

cronx "crontab-reminder/internal/cron"
)

func main() {
// Simulate the scheduler dispatch flow

// Job state
cronExpr := "0 */3 * * *"
tz := "Asia/Kuala_Lumpur"
nextRunAt := time.Date(2026, 3, 22, 7, 0, 0, 0, time.UTC) // 15:00 KL
lastRunAt := time.Date(2026, 3, 22, 4, 0, 0, 0, time.UTC) // 12:00 KL

loc, _ := time.LoadLocation(tz)

fmt.Printf("Job state:\n")
fmt.Printf("  next_run_at: %s (UTC) = %s (KL)\n", nextRunAt, nextRunAt.In(loc))
fmt.Printf("  last_run_at: %s (UTC) = %s (KL)\n", lastRunAt, lastRunAt.In(loc))

// Scenario 1: scheduler ticks at 06:30 UTC (14:30 KL)
nowUTC := time.Date(2026, 3, 22, 6, 30, 0, 0, time.UTC)
nowKL := nowUTC.In(loc)

fmt.Printf("\n=== SCENARIO 1: Tick at 06:30 UTC (14:30 KL) ===\n")
fmt.Printf("Scheduler tick at: %s (UTC) = %s (KL)\n", nowUTC, nowKL)

// Truncate to minute (what runOnce does)
nowMinute := nowUTC.UTC().Truncate(time.Minute)
fmt.Printf("Now minute: %s\n", nowMinute)

// ListDueJobs checks: enabled=1 AND next_run_at <= nowMinute
isDue := nextRunAt.Before(nowMinute) || nextRunAt.Equal(nowMinute)
fmt.Printf("ListDueJobs check: next_run_at(%s) <= nowMinute(%s) = %v\n",
nextRunAt, nowMinute, isDue)
fmt.Printf("-> Job NOT returned from ListDueJobs at 14:30 KL (not yet due)\n")

// Scenario 2: scheduler ticks at 07:00 UTC (15:00 KL)
nowUTC2 := time.Date(2026, 3, 22, 7, 0, 0, 0, time.UTC)
nowKL2 := nowUTC2.In(loc)
nowMinute2 := nowUTC2.UTC().Truncate(time.Minute)

fmt.Printf("\n=== SCENARIO 2: Tick at 07:00 UTC (15:00 KL) ===\n")
fmt.Printf("Scheduler tick at: %s (UTC) = %s (KL)\n", nowUTC2, nowKL2)
fmt.Printf("Now minute: %s\n", nowMinute2)

isDue2 := nextRunAt.Before(nowMinute2) || nextRunAt.Equal(nowMinute2)
fmt.Printf("ListDueJobs check: next_run_at(%s) <= nowMinute(%s) = %v\n",
nextRunAt, nowMinute2, isDue2)
fmt.Printf("-> Job IS returned from ListDueJobs (now due)\n")

// Now in dispatchJob
scheduledAt := nextRunAt  // This is 07:00 UTC

fmt.Printf("\nIn dispatchJob:\n")
fmt.Printf("  scheduledAt: %s (UTC) = %s (KL)\n", scheduledAt, scheduledAt.In(loc))
fmt.Printf("  nowUTC: %s (UTC) = %s (KL)\n", nowUTC2, nowUTC2.In(loc))

// Call NextRunAfter(expr, tz, scheduledAt, nowUTC)
nextRunAfter, _ := cronx.NextRunAfter(cronExpr, tz, scheduledAt, nowUTC2)

fmt.Printf("\nNextRunAfter(expr, tz, %s, %s):\n", scheduledAt, nowUTC2)
fmt.Printf("  Result: %s (UTC) = %s (KL)\n", nextRunAfter, nextRunAfter.In(loc))

fmt.Println("\n=== WHAT THE USER SEES ===")
fmt.Println("Reminder created with:")
fmt.Println("  - cron: '0 */3 * * *' (every 3 hours)")
fmt.Println("  - timezone: 'Asia/Kuala_Lumpur'")
fmt.Println("  - enabled: true")
fmt.Println("")
fmt.Println("Expected schedule in KL local time:")
fmt.Println("  00:00, 03:00, 06:00, 09:00, 12:00, 15:00, 18:00, 21:00")
fmt.Println("")
fmt.Println("So job should run at 12:00 KL, then 15:00 KL, then 18:00 KL, etc.")
fmt.Println("")
fmt.Printf("Current state shows:\n")
fmt.Printf("  - last_run_at: 04:00 UTC (12:00 KL) ✓ Correct\n")
fmt.Printf("  - next_run_at: 07:00 UTC (15:00 KL) ✓ Correct\n")
fmt.Println("")
fmt.Println("=== The ACTUAL Problem ===")
fmt.Println("")
fmt.Println("The logic seems correct! If last_run_at was 12:00 KL and")
fmt.Println("next_run_at is 15:00 KL, then the system IS working correctly")
fmt.Println("for scheduling future runs.")
fmt.Println("")
fmt.Println("The question is: Did the scheduler MISS running it at 12:00 KL?")
fmt.Println("")
fmt.Println("Looking at the data:")
fmt.Println("  - next_run_at: 07:00 UTC (15:00 KL) - future")
fmt.Println("  - last_run_at: 04:00 UTC (12:00 KL) - past")
fmt.Println("")
fmt.Println("If 12:00 KL already executed (last_run_at), then next 3h interval")
fmt.Println("should be 15:00 KL, which is correct.")
fmt.Println("")
fmt.Println("BUT if 12:00 KL was SUPPOSED to run but DIDN'T, then that's the bug!")
fmt.Println("The issue would be: 'Why didn't it run at 12:00 KL?'")
fmt.Println("")
fmt.Println("This could happen if:")
fmt.Println("  1. next_run_at was already past 12:00 KL before the execution")
fmt.Println("  2. Scheduler was not running at 12:00 KL")
fmt.Println("  3. There's a race condition or DST issue")
}
