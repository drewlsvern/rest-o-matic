// Package schedule computes calendar-aligned due-ness for named policy
// schedules (hourly, daily, weekly), so that "due" means "the current
// wall-clock boundary hasn't been run yet" rather than "N minutes have
// elapsed since the last run". That single comparison also gives catch-up
// behavior for free: however many boundaries were missed, the next check
// simply finds the current boundary differs from the last recorded one and
// reports due exactly once.
package schedule

import (
	"fmt"
	"time"
)

// Truncate rounds t down to the start of the calendar period named by
// schedule, computed in UTC to avoid daylight-saving-time ambiguity.
func Truncate(t time.Time, sched string) (time.Time, error) {
	u := t.UTC()
	switch sched {
	case "hourly":
		return time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), 0, 0, 0, time.UTC), nil
	case "daily":
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC), nil
	case "weekly":
		day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
		return day.AddDate(0, 0, -int(day.Weekday())), nil // back up to the most recent Sunday
	default:
		return time.Time{}, fmt.Errorf("unknown schedule %q", sched)
	}
}

// Due reports whether a job on the given schedule should run now, given
// when it last ran. A zero lastRun (never run before) is always due.
func Due(sched string, lastRun, now time.Time) (bool, error) {
	if lastRun.IsZero() {
		return true, nil
	}
	current, err := Truncate(now, sched)
	if err != nil {
		return false, err
	}
	last, err := Truncate(lastRun, sched)
	if err != nil {
		return false, err
	}
	return !current.Equal(last), nil
}
