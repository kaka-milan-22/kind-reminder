package cron

import (
	"fmt"
	"time"

	robfigcron "github.com/robfig/cron/v3"
)

var parser = robfigcron.NewParser(
	robfigcron.Minute |
		robfigcron.Hour |
		robfigcron.Dom |
		robfigcron.Month |
		robfigcron.Dow,
)

func Parse(expr string) (robfigcron.Schedule, error) {
	s, err := parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	return s, nil
}

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

func NextRunAfter(expr, tz string, from, afterUTC time.Time) (time.Time, error) {
	next, err := NextRun(expr, tz, from.UTC())
	if err != nil {
		return time.Time{}, err
	}
	for i := 0; i < 10000 && !next.After(afterUTC.UTC()); i++ {
		next, err = NextRun(expr, tz, next)
		if err != nil {
			return time.Time{}, err
		}
	}
	if !next.After(afterUTC.UTC()) {
		return time.Time{}, fmt.Errorf("cannot calculate next run after %s", afterUTC.UTC())
	}
	return next, nil
}

func ValidateTimezone(tz string) error {
	_, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	return nil
}
