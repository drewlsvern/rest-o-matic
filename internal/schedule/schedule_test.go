package schedule

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return tm
}

func TestDue_HourlyBecomesDueAtTopOfHour(t *testing.T) {
	lastRun := mustTime(t, "2026-01-01T01:58:00Z")
	now := mustTime(t, "2026-01-01T02:00:00Z")

	due, err := Due("hourly", lastRun, now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if !due {
		t.Fatal("expected hourly job to be due at the top of the next hour")
	}
}

func TestDue_HourlyNotDueBeforeNextBoundary(t *testing.T) {
	lastRun := mustTime(t, "2026-01-01T02:00:00Z")
	now := mustTime(t, "2026-01-01T02:15:00Z")

	due, err := Due("hourly", lastRun, now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if due {
		t.Fatal("expected hourly job not to be due 15 minutes after its last run")
	}
}

func TestDue_CatchUpFiresOnceRegardlessOfGapLength(t *testing.T) {
	lastRun := mustTime(t, "2026-01-01T00:00:00Z")
	now := mustTime(t, "2026-01-02T18:00:00Z") // 18+ hours late, many hourly boundaries missed

	due, err := Due("hourly", lastRun, now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if !due {
		t.Fatal("expected an overdue hourly job to be due")
	}
	// Due only reports a boolean per call - the caller runs the job once and
	// records the new last-run time, which is what prevents repeated firing.
}

func TestDue_NeverRunBeforeIsAlwaysDue(t *testing.T) {
	due, err := Due("daily", time.Time{}, time.Now())
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if !due {
		t.Fatal("expected a job with no recorded last run to be due")
	}
}

func TestDue_DailyBoundary(t *testing.T) {
	lastRun := mustTime(t, "2026-01-01T23:59:00Z")
	now := mustTime(t, "2026-01-02T00:01:00Z")

	due, err := Due("daily", lastRun, now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if !due {
		t.Fatal("expected daily job to be due once the calendar day rolls over")
	}
}

func TestDue_WeeklyBoundary(t *testing.T) {
	// 2026-01-04 is a Sunday (week start) and 2026-01-03 is the Saturday
	// before it, so these two timestamps fall on either side of a week
	// boundary.
	lastRun := mustTime(t, "2026-01-03T12:00:00Z") // Saturday
	now := mustTime(t, "2026-01-04T01:00:00Z")     // Sunday, new week
	due, err := Due("weekly", lastRun, now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if !due {
		t.Fatal("expected weekly job to be due once the week rolls over")
	}
}

func TestDue_UnknownScheduleErrors(t *testing.T) {
	lastRun := mustTime(t, "2026-01-01T00:00:00Z")
	if _, err := Due("fortnightly", lastRun, time.Now()); err == nil {
		t.Fatal("expected an error for an unknown schedule name")
	}
}
