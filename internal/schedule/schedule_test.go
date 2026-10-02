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

func TestNext_StartOfTheFollowingPeriod(t *testing.T) {
	lastRun := mustTime(t, "2026-01-07T14:10:00Z") // a Wednesday
	for sched, want := range map[string]string{
		"hourly": "2026-01-07T15:00:00Z",
		"daily":  "2026-01-08T00:00:00Z",
		"weekly": "2026-01-11T00:00:00Z", // the next Sunday
	} {
		got, err := Next(sched, lastRun)
		if err != nil {
			t.Fatalf("Next(%s): %v", sched, err)
		}
		if !got.Equal(mustTime(t, want)) {
			t.Errorf("Next(%s) = %v, want %s", sched, got, want)
		}
	}
}

func TestNext_UnknownSchedule(t *testing.T) {
	if _, err := Next("fortnightly", mustTime(t, "2026-01-07T14:10:00Z")); err == nil {
		t.Fatal("expected an error for an unknown schedule")
	}
}

// status shows Next as the due time, so it must agree with what tick does.
func TestNext_AgreesWithDue(t *testing.T) {
	lastRun := mustTime(t, "2026-01-07T14:10:00Z")
	for _, sched := range []string{"hourly", "daily", "weekly"} {
		next, err := Next(sched, lastRun)
		if err != nil {
			t.Fatalf("Next(%s): %v", sched, err)
		}
		for _, c := range []struct {
			now  time.Time
			want bool
		}{
			{lastRun, false},
			{next.Add(-time.Second), false},
			{next, true},
			{next.Add(100 * time.Hour), true},
		} {
			due, err := Due(sched, lastRun, c.now)
			if err != nil {
				t.Fatalf("Due(%s): %v", sched, err)
			}
			if due != c.want {
				t.Errorf("Due(%s, lastRun, %v) = %v, want %v (Next = %v)", sched, c.now, due, c.want, next)
			}
		}
	}
}
