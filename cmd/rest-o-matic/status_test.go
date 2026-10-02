package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// statusConfig has one job per schedule plus a second daily one, each with
// the repositories given.
func statusConfig() *config.Config {
	job := func(policy string, repos ...string) config.Job {
		j := config.Job{Policy: policy, Source: config.Source{Paths: []string{"/data"}}}
		for _, r := range repos {
			j.Repositories = append(j.Repositories, config.RepositoryRef{Name: r})
		}
		return j
	}
	return &config.Config{
		MaxConcurrent: 2,
		Policies: map[string]config.Policy{
			"hourly": {Schedule: "hourly"},
			"daily":  {Schedule: "daily"},
			"weekly": {Schedule: "weekly"},
		},
		Repositories: map[string]config.Repository{"nas": {}, "offsite": {}},
		Backups: map[string]config.Job{
			"postgres":  job("hourly", "nas"),
			"documents": job("daily", "nas"),
			"gitea":     job("daily", "nas", "offsite"),
			"media":     job("weekly", "nas"),
		},
	}
}

func notHeld(string) (bool, error) { return false, nil }

func heldJobs(names ...string) func(string) (bool, error) {
	return func(job string) (bool, error) {
		for _, n := range names {
			if n == job {
				return true, nil
			}
		}
		return false, nil
	}
}

func jobNamed(t *testing.T, r statusReport, name string) jobStatus {
	t.Helper()
	for _, j := range r.Jobs {
		if j.Name == name {
			return j
		}
	}
	t.Fatalf("no job %q in the report", name)
	return jobStatus{}
}

func mustStatus(t *testing.T, st *state.State, now time.Time, only string, held func(string) (bool, error)) statusReport {
	t.Helper()
	if st.Jobs == nil {
		st.Jobs = map[string]state.JobState{}
	}
	r, err := buildStatus(statusConfig(), st, now, only, held)
	if err != nil {
		t.Fatalf("buildStatus: %v", err)
	}
	return r
}

func render(r statusReport) string {
	var b bytes.Buffer
	writeStatus(&b, r, color.NewPainter(false), time.UTC)
	return b.String()
}

// withRuns builds a job's state the way RecordRun leaves it.
func withRuns(runs ...state.RunRecord) state.JobState {
	return state.JobState{LastRun: runs[0].Finished, LastOutcome: runs[0].Outcome, Runs: runs}
}

func TestStatus_ListsEveryJobInNameOrder(t *testing.T) {
	r := mustStatus(t, &state.State{}, ts(t, "2026-10-01T19:04:00Z"), "", notHeld)

	var names []string
	for _, j := range r.Jobs {
		names = append(names, j.Name)
	}
	if got := strings.Join(names, " "); got != "documents gitea media postgres" {
		t.Fatalf("got jobs %q, want them all in name order", got)
	}
	gitea := jobNamed(t, r, "gitea")
	if gitea.Schedule != "daily" || strings.Join(gitea.Repositories, " ") != "nas offsite" {
		t.Fatalf("got gitea %+v, want its schedule and repositories", gitea)
	}
}

func TestStatus_SuccessfulRunShowsOutcomeAndDuration(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"documents": withRuns(state.RunRecord{
			Started:      ts(t, "2026-10-01T02:00:05Z"),
			Finished:     ts(t, "2026-10-01T02:01:17Z"),
			Outcome:      state.OutcomeSuccess,
			Trigger:      state.TriggerTick,
			Repositories: []state.RepoResult{{Name: "nas", Result: state.ResultOK, SnapshotID: "a1b2c3d4"}},
		}),
	}}
	r := mustStatus(t, st, ts(t, "2026-10-01T08:04:00Z"), "", notHeld)

	run := jobNamed(t, r, "documents").LastRun
	if run == nil || run.Outcome != state.OutcomeSuccess || run.Error != nil {
		t.Fatalf("got last run %+v, want a success with no error", run)
	}
	if got := duration(*run); got != "1m12s" {
		t.Fatalf("got duration %q, want 1m12s", got)
	}
	want := "documents  daily     6 hours ago  ok       1m12s  in 15 hours"
	if out := render(r); !strings.Contains(out, want) {
		t.Fatalf("expected the table to contain %q, got:\n%s", want, out)
	}
}

func TestStatus_NeverRunJobIsDue(t *testing.T) {
	r := mustStatus(t, &state.State{}, ts(t, "2026-10-01T19:04:00Z"), "", notHeld)

	media := jobNamed(t, r, "media")
	if media.LastRun != nil || !media.Due || media.NextDue != nil {
		t.Fatalf("got %+v, want no last run, due, and no next due time", media)
	}
	want := "media      weekly    never     -        -     due now"
	if out := render(r); !strings.Contains(out, want) {
		t.Fatalf("expected the table to contain %q, got:\n%s", want, out)
	}
}

func TestStatus_FailedRunShowsEachRepository(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"gitea": withRuns(state.RunRecord{
			Started:  ts(t, "2026-10-01T17:00:00Z"),
			Finished: ts(t, "2026-10-01T17:00:48Z"),
			Outcome:  state.OutcomeFailed,
			Trigger:  state.TriggerTick,
			Error:    "repository offsite: restic backup failed: exit status 3",
			Repositories: []state.RepoResult{
				{Name: "nas", Result: state.ResultOK, SnapshotID: "a1b2c3d4"},
				{Name: "offsite", Result: state.ResultBackupFailed, Error: "restic backup failed: exit status 3"},
			},
		}),
	}}
	out := render(mustStatus(t, st, ts(t, "2026-10-01T19:04:00Z"), "", notHeld))

	for _, want := range []string{
		"gitea      daily     2 hours ago  FAILED   48s   in 4 hours",
		"  nas: ok (snapshot a1b2c3d4)",
		"  offsite: backup failed: restic backup failed: exit status 3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "repository offsite:") {
		t.Errorf("the run's own error repeats what the repository line says:\n%s", out)
	}
}

// The table abbreviates what the JSON document carries in full.
func TestStatus_TableShortensSnapshotIDsAndLongErrors(t *testing.T) {
	id := strings.Repeat("a1b2c3d4", 8)
	long := "restic backup failed: " + strings.Repeat("x", 400)
	st := &state.State{Jobs: map[string]state.JobState{
		"gitea": withRuns(state.RunRecord{
			Started:  ts(t, "2026-10-01T17:00:00Z"),
			Finished: ts(t, "2026-10-01T17:00:48Z"),
			Outcome:  state.OutcomeFailed,
			Trigger:  state.TriggerTick,
			Error:    "repository offsite: " + long,
			Repositories: []state.RepoResult{
				{Name: "nas", Result: state.ResultOK, SnapshotID: id},
				{Name: "offsite", Result: state.ResultBackupFailed, Error: long},
			},
		}),
	}}
	r := mustStatus(t, st, ts(t, "2026-10-01T19:04:00Z"), "", notHeld)
	out := render(r)

	if !strings.Contains(out, "  nas: ok (snapshot a1b2c3d4)\n") {
		t.Errorf("expected an eight-character snapshot ID, got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > maxNoteLen+3 {
			t.Errorf("a line of %d characters was not clipped: %s", n, line)
		}
	}
	if !strings.Contains(out, "…") {
		t.Errorf("expected the long error to end with an ellipsis, got:\n%s", out)
	}

	repos := jobNamed(t, r, "gitea").LastRun.Repositories
	if *repos[0].SnapshotID != id || *repos[1].Error != long {
		t.Error("the report itself must keep the full snapshot ID and error")
	}
}

func TestStatus_BeforeHookFailureShowsItsErrorAndNoRepositories(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"gitea": withRuns(state.RunRecord{
			Started:  ts(t, "2026-10-01T17:00:00Z"),
			Finished: ts(t, "2026-10-01T17:00:01Z"),
			Outcome:  state.OutcomeFailed,
			Trigger:  state.TriggerTick,
			Error:    `hook "stop-app.sh" failed: exit status 1`,
		}),
	}}
	r := mustStatus(t, st, ts(t, "2026-10-01T19:04:00Z"), "", notHeld)

	if run := jobNamed(t, r, "gitea").LastRun; len(run.Repositories) != 0 {
		t.Fatalf("got repository results %+v, want none", run.Repositories)
	}
	if out := render(r); !strings.Contains(out, `  hook "stop-app.sh" failed: exit status 1`) {
		t.Fatalf("expected the hook's error beneath the job, got:\n%s", out)
	}
}

func TestStatus_NextDueMatchesTick(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"postgres": withRuns(state.RunRecord{
			Started:  ts(t, "2026-10-01T14:09:50Z"),
			Finished: ts(t, "2026-10-01T14:10:00Z"),
			Outcome:  state.OutcomeSuccess,
			Trigger:  state.TriggerTick,
		}),
	}}

	before := mustStatus(t, st, ts(t, "2026-10-01T14:30:00Z"), "", notHeld)
	job := jobNamed(t, before, "postgres")
	if job.Due || job.NextDue == nil || !job.NextDue.Equal(ts(t, "2026-10-01T15:00:00Z")) {
		t.Fatalf("at 14:30 got due=%v next=%v, want not due, next due 15:00", job.Due, job.NextDue)
	}
	if out := render(before); !strings.Contains(out, "in 30 minutes") {
		t.Errorf("expected the table to say when the job is next due, got:\n%s", out)
	}

	after := mustStatus(t, st, ts(t, "2026-10-01T15:20:00Z"), "", notHeld)
	if job := jobNamed(t, after, "postgres"); !job.Due {
		t.Fatal("at 15:20 the job should be due")
	}
	if out := render(after); !strings.Contains(out, "due since 15:00") {
		t.Errorf("expected the table to say since when the job is due, got:\n%s", out)
	}
}

func TestStatus_DueSinceAnEarlierDayShowsTheDate(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"documents": withRuns(state.RunRecord{Finished: ts(t, "2026-09-28T02:01:00Z"), Outcome: state.OutcomeSuccess}),
	}}
	out := render(mustStatus(t, st, ts(t, "2026-10-01T19:04:00Z"), "", notHeld))
	if !strings.Contains(out, "3 days ago") || !strings.Contains(out, "due since Sep 29 00:00") {
		t.Fatalf("expected a dated due time for a job overdue by days, got:\n%s", out)
	}
}

func TestStatus_RunningWaitingAndStaleMarker(t *testing.T) {
	now := ts(t, "2026-10-01T19:04:00Z")
	marker := &state.Running{Started: ts(t, "2026-10-01T18:54:00Z"), Trigger: state.TriggerTick}
	st := &state.State{Jobs: map[string]state.JobState{
		"gitea":     {Running: marker}, // lock held: running
		"documents": {Running: marker}, // lock free: left by a killed process
		"postgres":  {},                // lock held, not started: waiting
	}}
	r := mustStatus(t, st, now, "", heldJobs("gitea", "postgres"))

	gitea := jobNamed(t, r, "gitea")
	if gitea.Running == nil || !gitea.Running.Started.Equal(marker.Started) || gitea.Waiting {
		t.Errorf("gitea: got running=%+v waiting=%v, want running since the marker's start", gitea.Running, gitea.Waiting)
	}
	if stale := jobNamed(t, r, "documents"); stale.Running != nil || stale.Waiting {
		t.Errorf("documents: got running=%+v waiting=%v, want neither once the process is gone", stale.Running, stale.Waiting)
	}
	if waiting := jobNamed(t, r, "postgres"); waiting.Running != nil || !waiting.Waiting {
		t.Errorf("postgres: got running=%+v waiting=%v, want waiting", waiting.Running, waiting.Waiting)
	}

	out := render(r)
	for _, want := range []string{
		"  running since 10 minutes ago (started by tick)",
		"  waiting for a repository or a free slot",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Count(out, "running since") != 1 {
		t.Errorf("only the job whose lock is held should be shown as running, got:\n%s", out)
	}
}

func TestStatus_LockProbeErrorIsReported(t *testing.T) {
	probe := func(string) (bool, error) { return false, errors.New("permission denied") }
	if _, err := buildStatus(statusConfig(), &state.State{Jobs: map[string]state.JobState{}}, time.Now(), "", probe); err == nil {
		t.Fatal("expected an error when a job's lock can't be checked")
	}
}

func TestStatus_LastTick(t *testing.T) {
	now := ts(t, "2026-10-01T19:04:00Z")

	if out := render(mustStatus(t, &state.State{}, now, "", notHeld)); !strings.HasPrefix(out, "last tick: never\n") {
		t.Errorf("with no tick recorded, got:\n%s", out)
	}

	tick := ts(t, "2026-10-01T19:02:00Z")
	r := mustStatus(t, &state.State{LastTick: &tick}, now, "", notHeld)
	if r.LastTick == nil || !r.LastTick.Equal(tick) {
		t.Fatalf("got last tick %v, want %v", r.LastTick, tick)
	}
	if out := render(r); !strings.HasPrefix(out, "last tick: 2 minutes ago\n") {
		t.Errorf("with a tick two minutes ago, got:\n%s", out)
	}
}

func TestStatus_SingleJobHistoryNewestFirst(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"postgres": withRuns(
			state.RunRecord{
				Started: ts(t, "2026-10-01T18:00:02Z"), Finished: ts(t, "2026-10-01T18:00:10Z"),
				Outcome: state.OutcomeFailed, Trigger: state.TriggerRun, Error: "interrupted by SIGTERM",
			},
			state.RunRecord{
				Started: ts(t, "2026-10-01T17:00:01Z"), Finished: ts(t, "2026-10-01T17:00:09Z"),
				Outcome: state.OutcomeSuccess, Trigger: state.TriggerTick,
				Repositories: []state.RepoResult{{Name: "nas", Result: state.ResultOK, SnapshotID: "feedc0de"}},
			},
		),
	}}
	r := mustStatus(t, st, ts(t, "2026-10-01T18:30:00Z"), "postgres", notHeld)

	if len(r.Jobs) != 1 || len(r.Jobs[0].Runs) != 2 {
		t.Fatalf("got %d jobs with %d runs, want only postgres with both runs", len(r.Jobs), len(r.Jobs[0].Runs))
	}
	if first := r.Jobs[0].Runs[0]; first.Outcome != state.OutcomeFailed || *first.Trigger != state.TriggerRun {
		t.Fatalf("got first run %+v, want the newer, failed one", first)
	}

	var b bytes.Buffer
	writeJobHistory(&b, r, color.NewPainter(false), time.UTC)
	out := b.String()
	for _, want := range []string{
		"job postgres: hourly, repositories nas",
		"next due: in 30 minutes",
		"2026-10-01 18:00  8s    FAILED   run",
		"  interrupted by SIGTERM",
		"2026-10-01 17:00  8s    ok       tick",
		"  nas: ok (snapshot feedc0de)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the history to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Index(out, "18:00  8s") > strings.Index(out, "17:00  8s") {
		t.Errorf("expected the newer run first, got:\n%s", out)
	}
}

func TestStatus_AllJobsReportHasNoHistory(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"postgres": withRuns(state.RunRecord{Finished: ts(t, "2026-10-01T18:00:10Z"), Outcome: state.OutcomeSuccess}),
	}}
	r := mustStatus(t, st, ts(t, "2026-10-01T18:30:00Z"), "", notHeld)
	if runs := jobNamed(t, r, "postgres").Runs; runs != nil {
		t.Fatalf("got %d runs in the all-jobs report, want history only for a single job", len(runs))
	}
}

// A state file from an earlier version has only the finish time and
// outcome of each job's last run.
func TestStatus_EarlierStateFormat(t *testing.T) {
	st := &state.State{Jobs: map[string]state.JobState{
		"documents": {LastRun: ts(t, "2026-10-01T17:01:00Z"), LastOutcome: "failed"},
	}}
	now := ts(t, "2026-10-01T19:04:00Z")
	r := mustStatus(t, st, now, "", notHeld)

	run := jobNamed(t, r, "documents").LastRun
	if run == nil || !run.Finished.Equal(ts(t, "2026-10-01T17:01:00Z")) || run.Outcome != "failed" {
		t.Fatalf("got last run %+v, want the recorded time and outcome", run)
	}
	if run.Started != nil || run.Trigger != nil || run.Error != nil {
		t.Fatalf("got %+v, want no start, trigger or error for an earlier-format record", run)
	}
	want := "documents  daily     2 hours ago  FAILED   -     in 4 hours"
	if out := render(r); !strings.Contains(out, want) {
		t.Fatalf("expected the table to contain %q, got:\n%s", want, out)
	}

	var b bytes.Buffer
	writeJobHistory(&b, mustStatus(t, st, now, "documents", notHeld), color.NewPainter(false), time.UTC)
	if out := b.String(); !strings.Contains(out, "2026-10-01 17:01 (finished)  -     FAILED   -") {
		t.Fatalf("expected the history to show the one earlier-format run, got:\n%s", out)
	}
}

func TestStatus_JSONShape(t *testing.T) {
	tick := ts(t, "2026-10-01T14:03:00-05:00")
	st := &state.State{
		LastTick: &tick,
		Jobs: map[string]state.JobState{
			"gitea": withRuns(state.RunRecord{
				Started:  ts(t, "2026-10-01T12:00:00-05:00"),
				Finished: ts(t, "2026-10-01T12:00:48-05:00"),
				Outcome:  state.OutcomeFailed,
				Trigger:  state.TriggerTick,
				Error:    "repository offsite: restic backup failed",
				Repositories: []state.RepoResult{
					{Name: "nas", Result: state.ResultOK, SnapshotID: "a1b2c3d4"},
					{Name: "offsite", Result: state.ResultBackupFailed, Error: "restic backup failed"},
				},
			}),
		},
	}
	now := ts(t, "2026-10-01T14:04:00-05:00")

	decode := func(only string) map[string]any {
		data, err := json.Marshal(mustStatus(t, st, now, only, notHeld))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}

	doc := decode("")
	if doc["format_version"] != float64(1) {
		t.Errorf("got format_version %v, want 1", doc["format_version"])
	}
	// Times are UTC whatever offset the state file recorded.
	if doc["generated_at"] != "2026-10-01T19:04:00Z" || doc["last_tick"] != "2026-10-01T19:03:00Z" {
		t.Errorf("got generated_at=%v last_tick=%v, want both in UTC", doc["generated_at"], doc["last_tick"])
	}

	jobs := doc["jobs"].([]any)
	if len(jobs) != 4 {
		t.Fatalf("got %d jobs, want 4", len(jobs))
	}
	byName := map[string]map[string]any{}
	for _, j := range jobs {
		job := j.(map[string]any)
		byName[job["name"].(string)] = job
	}

	// Absent values are null, not missing.
	media := byName["media"]
	for _, key := range []string{"next_due", "running", "last_run"} {
		if v, ok := media[key]; !ok || v != nil {
			t.Errorf("media[%q] = %v (present=%v), want an explicit null", key, v, ok)
		}
	}
	if media["due"] != true || media["waiting"] != false {
		t.Errorf("got media due=%v waiting=%v, want true and false", media["due"], media["waiting"])
	}
	if _, ok := media["runs"]; ok {
		t.Error("the all-jobs document should not carry run history")
	}

	run := byName["gitea"]["last_run"].(map[string]any)
	if run["outcome"] != "failed" || run["started"] != "2026-10-01T17:00:00Z" || run["error"] != "repository offsite: restic backup failed" {
		t.Errorf("got gitea last_run %v, want the failed run in UTC with its error", run)
	}
	repos := run["repositories"].([]any)
	nas, offsite := repos[0].(map[string]any), repos[1].(map[string]any)
	if nas["result"] != "ok" || nas["snapshot_id"] != "a1b2c3d4" || nas["error"] != nil {
		t.Errorf("got nas %v, want ok with its snapshot and a null error", nas)
	}
	if offsite["result"] != "backup_failed" || offsite["error"] != "restic backup failed" || offsite["snapshot_id"] != nil {
		t.Errorf("got offsite %v, want backup_failed with its error and a null snapshot", offsite)
	}

	single := decode("gitea")["jobs"].([]any)
	if len(single) != 1 {
		t.Fatalf("got %d jobs for a single-job document, want 1", len(single))
	}
	if runs, ok := single[0].(map[string]any)["runs"].([]any); !ok || len(runs) != 1 {
		t.Errorf("got runs %v, want the job's one recorded run", single[0].(map[string]any)["runs"])
	}
	if runs, ok := decode("media")["jobs"].([]any)[0].(map[string]any)["runs"].([]any); !ok || len(runs) != 0 {
		t.Errorf("got runs %v for a job that never ran, want an empty list", runs)
	}
}

func TestRunRecord_FromJobResult(t *testing.T) {
	started, finished := ts(t, "2026-10-01T17:00:00Z"), ts(t, "2026-10-01T17:01:12Z")

	t.Run("success", func(t *testing.T) {
		rec := runRecord(execution.JobResult{
			Job:   "documents",
			Repos: []execution.RepoOutcome{{Repository: "nas", SnapshotID: "a1b2c3d4"}},
		}, started, finished, state.TriggerRun)
		if rec.Outcome != state.OutcomeSuccess || rec.Error != "" || rec.Trigger != state.TriggerRun {
			t.Fatalf("got %+v, want a success started by run with no error", rec)
		}
		if !rec.Started.Equal(started) || !rec.Finished.Equal(finished) {
			t.Fatalf("got started=%v finished=%v", rec.Started, rec.Finished)
		}
		if got := rec.Repositories[0]; got.Result != state.ResultOK || got.SnapshotID != "a1b2c3d4" || got.Error != "" {
			t.Fatalf("got repository %+v, want ok with its snapshot ID", got)
		}
	})

	t.Run("one repository of each kind", func(t *testing.T) {
		rec := runRecord(execution.JobResult{
			Job: "gitea",
			Repos: []execution.RepoOutcome{
				{Repository: "nas", SnapshotID: "a1b2c3d4"},
				{Repository: "offsite", BackupErr: errors.New("restic backup failed:\n  exit status 3")},
				{Repository: "tape", SnapshotID: "feedc0de", ForgetErr: errors.New("restic forget failed")},
			},
		}, started, finished, state.TriggerTick)
		if rec.Outcome != state.OutcomeFailed || rec.Error != "repository offsite: restic backup failed: exit status 3" {
			t.Fatalf("got outcome=%q error=%q, want failed with a one-line error naming offsite", rec.Outcome, rec.Error)
		}
		want := []state.RepoResult{
			{Name: "nas", Result: state.ResultOK, SnapshotID: "a1b2c3d4"},
			{Name: "offsite", Result: state.ResultBackupFailed, Error: "restic backup failed: exit status 3"},
			{Name: "tape", Result: state.ResultForgetFailed, Error: "restic forget failed", SnapshotID: "feedc0de"},
		}
		for i, w := range want {
			if rec.Repositories[i] != w {
				t.Errorf("repository %d: got %+v, want %+v", i, rec.Repositories[i], w)
			}
		}
	})

	t.Run("before hook failed", func(t *testing.T) {
		rec := runRecord(execution.JobResult{Job: "gitea", HookErr: errors.New("hook failed")}, started, finished, state.TriggerTick)
		if rec.Outcome != state.OutcomeFailed || rec.Error != "hook failed" || len(rec.Repositories) != 0 {
			t.Fatalf("got %+v, want failed with the hook's error and no repositories", rec)
		}
	})
}
