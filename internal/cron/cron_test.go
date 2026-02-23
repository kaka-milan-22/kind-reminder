package cron

import (
	"testing"
	"time"
)

func TestNextRunEveryMinute(t *testing.T) {
	from := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	next, err := NextRun("*/1 * * * *", "UTC", from)
	if err != nil {
		t.Fatalf("NextRun error: %v", err)
	}
	want := time.Date(2026, 2, 21, 10, 1, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestNextRunHourlyOnTheHour(t *testing.T) {
	first, err := NextRun("0 * * * *", "UTC", time.Date(2026, 2, 21, 9, 59, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first NextRun error: %v", err)
	}
	if !first.Equal(time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("first = %s, want 10:00 UTC", first)
	}

	second, err := NextRun("0 * * * *", "UTC", first)
	if err != nil {
		t.Fatalf("second NextRun error: %v", err)
	}
	if !second.Equal(time.Date(2026, 2, 21, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("second = %s, want 11:00 UTC", second)
	}

	third, err := NextRun("0 * * * *", "UTC", second)
	if err != nil {
		t.Fatalf("third NextRun error: %v", err)
	}
	if !third.Equal(time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("third = %s, want 12:00 UTC", third)
	}
}

func TestNextRunDailyAtNineWithTimezone(t *testing.T) {
	// 2026-02-21 13:30 UTC == 08:30 in America/New_York (UTC-5).
	from := time.Date(2026, 2, 21, 13, 30, 0, 0, time.UTC)
	next, err := NextRun("0 9 * * *", "America/New_York", from)
	if err != nil {
		t.Fatalf("NextRun error: %v", err)
	}
	// Next local 09:00 should be 2026-02-21 14:00 UTC.
	want := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestNextRunDailyAtMidnight(t *testing.T) {
	from := time.Date(2026, 2, 21, 23, 50, 0, 0, time.UTC)
	next, err := NextRun("0 0 * * *", "UTC", from)
	if err != nil {
		t.Fatalf("NextRun error: %v", err)
	}
	want := time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}
