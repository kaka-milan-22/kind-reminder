package main

import (
"fmt"
"time"

cronx "crontab-reminder/internal/cron"
)

func main() {
// Reminder details:
// cron: "0 */3 * * *"  (every 3 hours at minute 0)
// timezone: Asia/Kuala_Lumpur (UTC+8)
// last_run_at: 2026-03-22T04:00:00Z (= KL 12:00)
// next_run_at: 2026-03-22T07:00:00Z (= KL 15:00)

// The cron "0 */3 * * *" should run at:
// UTC times: 00:00, 03:00, 06:00, 09:00, 12:00, 15:00, 18:00, 21:00
// KL times (UTC+8): 08:00, 11:00, 14:00, 17:00, 20:00, 23:00, 02:00 (next day), 05:00 (next day)

tz := "Asia/Kuala_Lumpur"
cronExpr := "0 */3 * * *"

// From last_run_at (04:00 UTC = 12:00 KL)
from := time.Date(2026, 3, 22, 4, 0, 0, 0, time.UTC)
fmt.Printf("From (UTC): %s\n", from)

loc, _ := time.LoadLocation(tz)
fmt.Printf("From (KL):  %s\n", from.In(loc))

// Calculate next run from that point
next, err := cronx.NextRun(cronExpr, tz, from)
if err != nil {
fmt.Printf("Error: %v\n", err)
return
}
fmt.Printf("\nNext run (UTC): %s\n", next)
fmt.Printf("Next run (KL):  %s\n", next.In(loc))

// What should the expected next runs be?
fmt.Println("\n--- Expected cron schedule for '0 */3 * * *' in KL timezone ---")
fmt.Println("UTC (00:00, 03:00, 06:00, 09:00, 12:00, 15:00, 18:00, 21:00)")
fmt.Println("KL  (08:00, 11:00, 14:00, 17:00, 20:00, 23:00, 02:00(+1), 05:00(+1))")

// The user says it should run at KL 12:00 (04:00 UTC)
// last_run_at is 04:00 UTC, next_run_at is 07:00 UTC
// This means: from 04:00 UTC, next cron occurrence is 07:00 UTC (15:00 KL)
// But shouldn't it have run at 06:00 UTC (14:00 KL) between 04:00 and 07:00?

fmt.Println("\n--- Testing if 06:00 UTC should be included ---")
from2 := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
for i := 0; i < 10; i++ {
next2, _ := cronx.NextRun(cronExpr, tz, from2)
fmt.Printf("From: %s (UTC) / %s (KL) -> Next: %s (UTC) / %s (KL)\n",
from2.Format("15:04 MST"), from2.In(loc).Format("15:04 MST"),
next2.Format("15:04 MST"), next2.In(loc).Format("15:04 MST"))
from2 = next2
}
}
