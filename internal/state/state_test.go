package state

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	return NewStore(filepath.Join(dir, "state.json"), filepath.Join(dir, "locks")), dir
}

func run(finished time.Time, outcome string) RunRecord {
	return RunRecord{Started: finished.Add(-time.Minute), Finished: finished, Outcome: outcome, Trigger: TriggerTick}
}

func mustLoad(t *testing.T, s *Store) *State {
	t.Helper()
	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return st
}

func TestLoad_MissingFileReturnsEmptyStateAndCreatesNothing(t *testing.T) {
	s, dir := newTestStore(t)

	st := mustLoad(t, s)
	if len(st.Jobs) != 0 || st.LastTick != nil {
		t.Fatalf("expected an empty state, got %+v", st)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Load created %d entries in the state directory", len(entries))
	}
}

func TestRecordRun_PersistsRecordAndLastRun(t *testing.T) {
	s, _ := newTestStore(t)
	finished := time.Now().UTC().Truncate(time.Second)
	rec := RunRecord{
		Started:  finished.Add(-72 * time.Second),
		Finished: finished,
		Outcome:  OutcomeFailed,
		Trigger:  TriggerRun,
		Error:    "repository offsite: restic backup failed",
		Repositories: []RepoResult{
			{Name: "nas", Result: ResultOK, SnapshotID: "a1b2c3d4"},
			{Name: "offsite", Result: ResultBackupFailed, Error: "restic backup failed"},
		},
	}
	if err := s.RecordRun("gitea", rec, Failing{}); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	got := mustLoad(t, s).Jobs["gitea"]
	if !got.LastRun.Equal(finished) || got.LastOutcome != OutcomeFailed {
		t.Fatalf("got LastRun=%v LastOutcome=%q, want %v and %q", got.LastRun, got.LastOutcome, finished, OutcomeFailed)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(got.Runs))
	}
	r := got.Runs[0]
	if !r.Started.Equal(rec.Started) || r.Trigger != TriggerRun || r.Error != rec.Error {
		t.Errorf("got run %+v, want %+v", r, rec)
	}
	if len(r.Repositories) != 2 || r.Repositories[0].SnapshotID != "a1b2c3d4" || r.Repositories[1].Result != ResultBackupFailed {
		t.Errorf("got repositories %+v, want %+v", r.Repositories, rec.Repositories)
	}
}

func TestRecordRun_HistoryIsNewestFirstAndCapped(t *testing.T) {
	s, _ := newTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < MaxRuns+3; i++ {
		rec := run(base.Add(time.Duration(i)*time.Hour), OutcomeSuccess)
		rec.Error = strconv.Itoa(i)
		if err := s.RecordRun("postgres", rec, Failing{}); err != nil {
			t.Fatalf("RecordRun %d: %v", i, err)
		}
	}

	runs := mustLoad(t, s).Jobs["postgres"].Runs
	if len(runs) != MaxRuns {
		t.Fatalf("got %d runs, want %d", len(runs), MaxRuns)
	}
	if newest, oldest := runs[0].Error, runs[MaxRuns-1].Error; newest != strconv.Itoa(MaxRuns+2) || oldest != "3" {
		t.Fatalf("got newest=%s oldest=%s, want newest=%d oldest=3", newest, oldest, MaxRuns+2)
	}
}

func TestRunningMarker_SetByMarkRunningClearedByRecordRun(t *testing.T) {
	s, _ := newTestStore(t)
	started := time.Now().UTC().Truncate(time.Second)

	if err := s.MarkRunning("gitea", started, TriggerTick); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	js := mustLoad(t, s).Jobs["gitea"]
	if js.Running == nil || !js.Running.Started.Equal(started) || js.Running.Trigger != TriggerTick {
		t.Fatalf("got running %+v, want started=%v trigger=tick", js.Running, started)
	}
	if !js.LastRun.IsZero() {
		t.Fatalf("a job that has only started has LastRun %v, want zero so it stays due", js.LastRun)
	}

	if err := s.RecordRun("gitea", run(started.Add(time.Minute), OutcomeSuccess), Failing{}); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	if js := mustLoad(t, s).Jobs["gitea"]; js.Running != nil {
		t.Fatalf("running marker still set after the run was recorded: %+v", js.Running)
	}
}

func TestClearRunning_RemovesAStaleMarkerAndKeepsHistory(t *testing.T) {
	s, _ := newTestStore(t)
	finished := time.Now().UTC().Truncate(time.Second)
	if err := s.RecordRun("gitea", run(finished, OutcomeSuccess), Failing{}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRunning("gitea", finished.Add(time.Hour), TriggerTick); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearRunning("gitea"); err != nil {
		t.Fatalf("ClearRunning: %v", err)
	}
	js := mustLoad(t, s).Jobs["gitea"]
	if js.Running != nil || len(js.Runs) != 1 || !js.LastRun.Equal(finished) {
		t.Fatalf("got %+v, want the marker gone and the earlier run kept", js)
	}
}

func TestClearRunning_WritesNothingWhenThereIsNoMarker(t *testing.T) {
	s, dir := newTestStore(t)

	if err := s.ClearRunning("gitea"); err != nil {
		t.Fatalf("ClearRunning: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err == nil {
		t.Fatal("ClearRunning created a state file for a job with no marker")
	}
}

func TestRecordTick_KeepsJobRecords(t *testing.T) {
	s, _ := newTestStore(t)
	finished := time.Now().UTC().Truncate(time.Second)
	if err := s.RecordRun("gitea", run(finished, OutcomeSuccess), Failing{}); err != nil {
		t.Fatal(err)
	}

	tick := finished.Add(time.Minute)
	if err := s.RecordTick(tick); err != nil {
		t.Fatalf("RecordTick: %v", err)
	}
	st := mustLoad(t, s)
	if st.LastTick == nil || !st.LastTick.Equal(tick) {
		t.Fatalf("got LastTick %v, want %v", st.LastTick, tick)
	}
	if len(st.Jobs["gitea"].Runs) != 1 {
		t.Fatal("recording a tick lost the job's run record")
	}
}

// A file written by an earlier version holds only last_run and
// last_outcome. It must load, and keep both until the job next runs.
func TestLoad_EarlierFormatIsReadAndPreserved(t *testing.T) {
	s, dir := newTestStore(t)
	old := `{
  "jobs": {
    "documents": {
      "last_run": "2026-09-30T02:00:00-05:00",
      "last_outcome": "failed"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 30, 7, 0, 0, 0, time.UTC)

	js := mustLoad(t, s).Jobs["documents"]
	if js.FailingSince != nil || js.FailureNotified != nil {
		t.Fatalf("an earlier-format record loaded with failing state: %+v", js)
	}
	if !js.LastRun.Equal(want) || js.LastOutcome != "failed" || len(js.Runs) != 0 {
		t.Fatalf("got %+v, want LastRun=%v LastOutcome=failed and no runs", js, want)
	}

	// Writing something else must not disturb it.
	if err := s.RecordTick(want.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	js = mustLoad(t, s).Jobs["documents"]
	if !js.LastRun.Equal(want) || js.LastOutcome != "failed" {
		t.Fatalf("after an unrelated write got %+v, want the earlier record unchanged", js)
	}
}

// Separate Stores on one path stand in for separate processes: each has its
// own mutex, so only the file lock keeps their updates from being lost.
func TestUpdates_FromSeparateStoresAllSurvive(t *testing.T) {
	_, dir := newTestStore(t)
	path, lockDir := filepath.Join(dir, "state.json"), filepath.Join(dir, "locks")
	finished := time.Now().UTC().Truncate(time.Second)

	const writers = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers+1)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- NewStore(path, lockDir).RecordRun("job-"+strconv.Itoa(i), run(finished, OutcomeSuccess), Failing{})
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- NewStore(path, lockDir).RecordTick(finished)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
	}

	st := mustLoad(t, NewStore(path, lockDir))
	if st.LastTick == nil {
		t.Error("the tick time was lost")
	}
	for i := 0; i < writers; i++ {
		if _, ok := st.Jobs["job-"+strconv.Itoa(i)]; !ok {
			t.Errorf("the record for job-%d was lost", i)
		}
	}
}

func TestFailingState_KeptWhileFailingClearedOnSuccess(t *testing.T) {
	s, _ := newTestStore(t)
	base := time.Date(2026, 1, 5, 2, 0, 0, 0, time.UTC)
	since, notified := base, base.Add(time.Minute)

	if err := s.RecordRun("gitea", run(base.Add(time.Minute), OutcomeFailed), Failing{Since: &since, Notified: &notified}); err != nil {
		t.Fatal(err)
	}
	// A later failure with no new notification carries both forward.
	if err := s.RecordRun("gitea", run(base.Add(time.Hour), OutcomeFailed), Failing{Since: &since, Notified: &notified}); err != nil {
		t.Fatal(err)
	}
	js := mustLoad(t, s).Jobs["gitea"]
	if js.FailingSince == nil || !js.FailingSince.Equal(since) || js.FailureNotified == nil || !js.FailureNotified.Equal(notified) {
		t.Fatalf("got failing_since=%v failure_notified=%v, want %v and %v", js.FailingSince, js.FailureNotified, since, notified)
	}

	// A success clears both, whatever it is given.
	if err := s.RecordRun("gitea", run(base.Add(2*time.Hour), OutcomeSuccess), Failing{Since: &since, Notified: &notified}); err != nil {
		t.Fatal(err)
	}
	js = mustLoad(t, s).Jobs["gitea"]
	if js.FailingSince != nil || js.FailureNotified != nil {
		t.Fatalf("got failing_since=%v failure_notified=%v after a success, want both cleared", js.FailingSince, js.FailureNotified)
	}
}

func TestFailingState_UnsentNotificationIsRecordedAsNil(t *testing.T) {
	s, _ := newTestStore(t)
	since := time.Date(2026, 1, 5, 2, 0, 0, 0, time.UTC)

	if err := s.RecordRun("gitea", run(since.Add(time.Minute), OutcomeFailed), Failing{Since: &since}); err != nil {
		t.Fatal(err)
	}
	js := mustLoad(t, s).Jobs["gitea"]
	if js.FailingSince == nil || js.FailureNotified != nil {
		t.Fatalf("got failing_since=%v failure_notified=%v, want a failing-since time and no notification time", js.FailingSince, js.FailureNotified)
	}
}
