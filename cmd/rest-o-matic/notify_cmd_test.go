//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
)

// notifying adds notification commands to cfg that log the kind and the
// job to the fixture's hook log.
func (f *concurrencyFixture) notifying(cfg *config.Config) *config.Config {
	say := func(kind string) []string {
		return []string{"echo " + kind + " $RESTOMATIC_JOB >> " + f.log}
	}
	cfg.Notify = config.Notify{Failure: say("ALERT"), Recovery: say("RECOVERED"), Success: say("PING")}
	return cfg
}

// notifications returns the notification lines logged so far, dropping the
// jobs' own start/end hook lines.
func (f *concurrencyFixture) notifications(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" && !strings.HasPrefix(line, "start ") && !strings.HasPrefix(line, "end ") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, ", ")
}

func (f *concurrencyFixture) runNow(t *testing.T, cfg *config.Config, name string) {
	t.Helper()
	if s := waitStarted(t, f.startJob(context.Background(), cfg, name)); s.kind != jobRan {
		t.Fatalf("job %s: got kind %v, want jobRan", name, s.kind)
	}
}

// The once-a-day limit only works if each run's failing state is recorded
// and read back for the next run.
func TestNotify_FailingStateCarriesBetweenRunsThroughTheStateFile(t *testing.T) {
	f := newConcurrencyFixture(t)
	fail := filepath.Join(f.dir, "fail")
	job := f.job("documents") // no repositories: it succeeds unless its before hook fails
	job.Hooks.Before = []string{"[ ! -e " + fail + " ]"}
	cfg := f.notifying(f.config(2, job))

	if err := os.WriteFile(fail, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f.runNow(t, cfg, "documents")
	f.runNow(t, cfg, "documents")
	f.runNow(t, cfg, "documents")
	if got := f.notifications(t); got != "ALERT documents" {
		t.Fatalf("after three failed runs got %q, want one alert", got)
	}
	st, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	js := st.Jobs["documents"]
	if js.FailingSince == nil || js.FailureNotified == nil {
		t.Fatalf("got failing_since=%v failure_notified=%v, want both recorded", js.FailingSince, js.FailureNotified)
	}
	if first := js.Runs[2].Started; js.FailingSince.Before(first) || js.FailingSince.After(js.Runs[2].Finished) {
		t.Errorf("failing_since %v is not within the first failed run (%v to %v)", js.FailingSince, first, js.Runs[2].Finished)
	}

	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	f.runNow(t, cfg, "documents")
	f.runNow(t, cfg, "documents")
	if got := f.notifications(t); got != "ALERT documents, RECOVERED documents, PING documents, PING documents" {
		t.Fatalf("after it was fixed got %q, want one recovery and a ping per success", got)
	}
	st, err = f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if js := st.Jobs["documents"]; js.FailingSince != nil || js.FailureNotified != nil {
		t.Fatalf("failing state not cleared after a success: %+v", js)
	}
}

func TestNotify_EachJobIsLimitedSeparately(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.notifying(f.config(2, f.job("documents", "nas"), f.job("gitea", "offsite")))

	f.runNow(t, cfg, "documents")
	f.runNow(t, cfg, "documents")
	f.runNow(t, cfg, "gitea")
	f.runNow(t, cfg, "gitea")
	if got := f.notifications(t); got != "ALERT documents, ALERT gitea" {
		t.Fatalf("got %q, want one alert for each job", got)
	}
}

func TestNotify_SkippedJobProducesNoNotification(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.notifying(f.config(2, f.job("documents", "nas")))

	held, ok, err := lock.AcquireJob(f.opts.LockDir, "documents")
	if err != nil || !ok {
		t.Fatalf("holding the job lock: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	attempts := dispatch(context.Background(), context.Background(), cfg, f.store, []string{"documents"}, f.opts)
	if attempts[0].kind != jobAlreadyRunning {
		t.Fatalf("got kind %v, want jobAlreadyRunning", attempts[0].kind)
	}
	if got := f.notifications(t); got != "" {
		t.Fatalf("a skipped job produced notifications: %q", got)
	}
}

// A job already failing when this version was installed has only its last
// outcome on record.
func TestNotify_JobFailingInAnEarlierFormatState(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.notifying(f.config(2, f.job("documents", "nas")))
	old := `{"jobs": {"documents": {"last_run": "2026-09-30T02:00:00Z", "last_outcome": "failed"}}}`
	statePath := filepath.Join(f.dir, "state", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	f.runNow(t, cfg, "documents")
	if got := f.notifications(t); got != "ALERT documents" {
		t.Fatalf("got %q, want an alert for a job with none recorded", got)
	}
	st, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if since := st.Jobs["documents"].FailingSince; since == nil || since.Format("2006-01-02") != "2026-09-30" {
		t.Fatalf("got failing_since %v, want the earlier failed run's time", since)
	}
}

// writeNotifyConfig writes a config with a top-level notify block and one
// job, documents, that always fails (its repository was never initialised).
func writeNotifyConfig(t *testing.T, workdir, notifyYAML string) string {
	t.Helper()
	path := filepath.Join(workdir, "rest-o-matic.yaml")
	content := `
notify:
` + notifyYAML + `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: ` + filepath.Join(workdir, "never-initialized") + `, password: x}
backups:
  documents:
    source: {paths: ["` + workdir + `"]}
    policy: hot
    repositories: [nas]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLI_NotifyFromTickAndRun(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	alerts := filepath.Join(workdir, "alerts")
	configPath := writeNotifyConfig(t, workdir, `  failure: ['echo "$RESTOMATIC_JOB|$RESTOMATIC_FAILED_REPOS" >> `+alerts+`']`)

	// tick finds the job due and it fails: one alert. A manual run minutes
	// later fails too, and stays quiet.
	if _, _, code := runCLI(t, bin, workdir, "--config", configPath, "tick"); code == 0 {
		t.Fatal("setup: expected the tick to fail against an uninitialised repository")
	}
	if _, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents"); code == 0 {
		t.Fatal("setup: expected the run to fail")
	}
	data, _ := os.ReadFile(alerts)
	if got := strings.TrimSpace(string(data)); got != "documents|nas" {
		t.Fatalf("got alerts %q, want exactly one, naming the job and repository", got)
	}

	stdout, _, _ := runCLI(t, bin, workdir, "--config", configPath, "status", "--json")
	if !contains(stdout, `"failing_since": "20`) {
		t.Errorf("expected status --json to report since when the job has been failing, got: %s", stdout)
	}
}

func TestCLI_NotifyRunAloneNotifies(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	alerts := filepath.Join(workdir, "alerts")
	configPath := writeNotifyConfig(t, workdir, `  failure: ["echo alert >> `+alerts+`"]`)

	runCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	if data, _ := os.ReadFile(alerts); strings.TrimSpace(string(data)) != "alert" {
		t.Fatalf("got alerts %q, want one from a manual run", data)
	}
}

func TestCLI_FailedNotificationIsReportedAndDoesNotChangeTheOutcome(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeNotifyConfig(t, workdir, `  failure: ["exit 7"]`)

	stdout, _, _ := runCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	if !contains(stdout, "job documents: FAILED") || !contains(stdout, "notification failed:") {
		t.Fatalf("expected the job's own failure and a notification warning, got: %s", stdout)
	}
	st, _ := os.ReadFile(filepath.Join(workdir, ".rest-o-matic", "state.json"))
	if contains(string(st), "failure_notified") {
		t.Errorf("a notification that failed was recorded as sent: %s", st)
	}
}

func TestCLI_ValidateRejectsUnknownNotifyKey(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeNotifyConfig(t, workdir, `  failed: ["echo alert"]`)

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "validate")
	if code == 0 || !contains(stderr, `unknown key "failed"`) {
		t.Fatalf("validate (exit %d): %s", code, stderr)
	}
}
