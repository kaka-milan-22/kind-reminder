package main

import (
"fmt"
"time"

robfigcron "github.com/robfig/cron/v3"
)

func main() {
// Parse the cron expression
parser := robfigcron.NewParser(
robfigcron.Minute |
robfigcron.Hour |
robfigcron.Dom |
robfigcron.Month |
robfigcron.Dow,
)

schedule, _ := parser.Parse("0 */3 * * *")

tz := "Asia/Kuala_Lumpur"
loc, _ := time.LoadLocation(tz)

// Test from 04:00 UTC (12:00 KL)
from := time.Date(2026, 3, 22, 4, 0, 0, 0, time.UTC)

fmt.Println("=== THE BUG ===")
fmt.Printf("From time (UTC):    %s\n", from)
fmt.Printf("From time (KL):     %s\n", from.In(loc))

// What robfig/cron does internally:
// It takes the "from" time, converts it to local time of the timezone
// Then calculates the next occurrence based on local time

fromLocal := from.In(loc)
fmt.Printf("\nCron converts to local: %s\n", fromLocal)

// robfig/cron.Schedule.Next() uses the input time to calculate next
// For cron "0 */3 * * *", it matches: minute=0, hour=0,3,6,9,12,15,18,21

// From 12:00 KL (minute 0), the next matching hour is 15:00 KL
nextLocal := schedule.Next(fromLocal)
fmt.Printf("Next local (cron):  %s\n", nextLocal)

nextUTC := nextLocal.UTC()
fmt.Printf("Next UTC (cron):    %s\n", nextUTC)

fmt.Println("\n=== THE PROBLEM ===")
fmt.Println("The cron expression '0 */3 * * *' means:")
fmt.Println("  - Every 3 hours")
fmt.Println("  - At minute 0")
fmt.Println("  - But the HOUR field is 0,3,6,9,12,15,18,21 in the TIMEZONE")

fmt.Println("\nWhen From=12:00 KL, robfig looks at:")
fmt.Println("  - Current local hour: 12")
fmt.Println("  - Matching hours: 0,3,6,9,12,15,18,21")
fmt.Println("  - Next match: 15:00 (same day)")

fmt.Println("\nBUT: The CRON SCHEDULE in KL is supposed to be:")
fmt.Println("  - 00:00, 03:00, 06:00, 09:00, 12:00, 15:00, 18:00, 21:00 (in KL)")
fmt.Println("  - NOT starting at UTC times!")

fmt.Println("\n=== THE ROOT CAUSE ===")
fmt.Println("robfig/cron evaluates cron expressions in the WALL CLOCK TIME of the timezone")
fmt.Println("So '0 */3 * * *' means 'every 3 hours' in KL local time")
fmt.Println("If last execution was at 12:00 KL (minute=0), the next at 15:00 KL (3 hours later)")
fmt.Println("")
fmt.Println("The issue is: should 'next 3 hour interval' mean:")
fmt.Println("  A) Next occurrence of the cron pattern (15:00 KL) <- What the code does")
fmt.Println("  B) Exactly 3 hours later in real time (15:00 KL) <- Same result but different logic")

fmt.Println("\nBUT WAIT... let's check what SHOULD happen with a fresh scheduler...")

from2 := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
from2Local := from2.In(loc)

fmt.Printf("\nStarting fresh from 00:00 UTC (08:00 KL):\n")
for i := 0; i < 8; i++ {
next := schedule.Next(from2Local)
fmt.Printf("  From: %s (KL) -> Next: %s (KL)\n",
from2Local.Format("15:04"), next.Format("15:04"))
from2Local = next
}
}
