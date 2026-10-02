//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// writeFailingJobConfig writes a config whose one job, documents, fails
// quickly because its repository was never initialised.
func writeFailingJobConfig(t *testing.T, workdir string) string {
	t.Helper()
	return writeSignalConfig(t, workdir, `
  documents:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
`)
}

// snapshotDir lists every path under dir with its size and modification
// time, to tell whether a command touched anything there.
func snapshotDir(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %d %s\n", path, info.Size(), info.ModTime())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestCLI_StatusWithNoStateReportsNeverRunAndCreatesNothing(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeFailingJobConfig(t, workdir)

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "status")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr)
	}
	if !contains(stdout, "last tick: never") || !contains(stdout, "never") || !contains(stdout, "due now") {
		t.Errorf("expected the job to be shown as never run and due, got: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".rest-o-matic")); err == nil {
		t.Error("status created the state directory")
	}
}

func TestCLI_StatusReportsFailedRunAndExitsZero(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeFailingJobConfig(t, workdir)

	if _, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents"); code == 0 {
		t.Fatal("setup: expected the run against an uninitialised repository to fail")
	}
	before := snapshotDir(t, filepath.Join(workdir, ".rest-o-matic"))

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "status")
	if code != 0 {
		t.Fatalf("expected exit 0 although a job failed, got %d (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"documents", "FAILED", "  nas: backup failed: restic backup failed"} {
		if !contains(stdout, want) {
			t.Errorf("expected stdout to contain %q, got: %s", want, stdout)
		}
	}
	// restic's own message, not the JSON it was wrapped in. With restic on
	// PATH that message says the repository's config file can't be opened;
	// the wording around it differs between restic versions.
	if contains(stdout, "message_type") {
		t.Errorf("status shows restic's raw JSON: %s", stdout)
	}
	if _, err := exec.LookPath("restic"); err == nil {
		state, _ := os.ReadFile(filepath.Join(workdir, ".rest-o-matic", "state.json"))
		for name, text := range map[string]string{"status": stdout, "the state file": string(state)} {
			if !contains(text, "unable to open config file") || contains(text, "message_type") {
				t.Errorf("expected %s to carry restic's plain \"unable to open config file\" message, got: %s", name, text)
			}
		}
	}

	history, _, code := runCLI(t, bin, workdir, "--config", configPath, "status", "documents")
	if code != 0 || !contains(history, "job documents: hourly, repositories nas") || !contains(history, "FAILED   run") {
		t.Errorf("expected the job's history with its one failed run (exit %d), got: %s", code, history)
	}

	if after := snapshotDir(t, filepath.Join(workdir, ".rest-o-matic")); after != before {
		t.Errorf("status changed the state directory:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCLI_StatusJSONIsOneDocumentEvenWithConfigWarnings(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "warn.yaml")
	content := `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: backups/restic, password: x}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [nas]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "status", "--json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr)
	}
	if !contains(stderr, "config warning:") {
		t.Errorf("expected the config warning on stderr, got: %s", stderr)
	}
	var doc struct {
		FormatVersion int `json:"format_version"`
		Jobs          []struct {
			Name string `json:"name"`
			Due  bool   `json:"due"`
		} `json:"jobs"`
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one JSON document: %s", stdout)
	}
	if doc.FormatVersion != 1 || len(doc.Jobs) != 1 || doc.Jobs[0].Name != "documents" || !doc.Jobs[0].Due {
		t.Errorf("got %+v, want format 1 and the one never-run job as due", doc)
	}
}

func TestCLI_StatusUnknownJob(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeFailingJobConfig(t, workdir)

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "status", "nosuchjob")
	if code == 0 {
		t.Fatal("expected a non-zero exit for a job that isn't in the config")
	}
	if !contains(stderr, `no such job "nosuchjob"`) {
		t.Errorf("expected stderr to name the job, got: %s", stderr)
	}
}

func TestCLI_StatusShowsRunningWhileTheJobLockIsHeld(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	started, release := filepath.Join(workdir, "started"), filepath.Join(workdir, "release")
	configPath := writeSignalConfig(t, workdir, `
  documents:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
    hooks:
      before: ["touch `+started+`; while [ ! -e `+release+` ]; do sleep 0.05; done"]
`)

	cmd, out := startCLI(t, bin, workdir, "--config", configPath, "tick")
	waitForFile(t, started)

	stdout, _, _ := runCLI(t, bin, workdir, "--config", configPath, "status")
	if !contains(stdout, "running since just now (started by tick)") {
		t.Errorf("expected the job to be shown as running, got: %s", stdout)
	}

	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatalf("setup: expected the tick to fail against an uninitialised repository, output: %s", out)
	}
	stdout, _, _ = runCLI(t, bin, workdir, "--config", configPath, "status")
	if contains(stdout, "running since") {
		t.Errorf("expected the job not to be shown as running once it finished, got: %s", stdout)
	}
}

func TestCLI_TickRecordsItsTimeEvenWithNothingDue(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeFailingJobConfig(t, workdir)
	statePath := filepath.Join(workdir, ".rest-o-matic", "state.json")
	lastTick := func() string {
		t.Helper()
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var st struct {
			LastTick string `json:"last_tick"`
		}
		if err := json.Unmarshal(data, &st); err != nil {
			t.Fatal(err)
		}
		return st.LastTick
	}

	// The job fails, is recorded, and so is not due on the second tick.
	runCLI(t, bin, workdir, "--config", configPath, "tick")
	first := lastTick()
	if first == "" {
		t.Fatal("the first tick recorded no time")
	}
	stdout, _, code := runCLI(t, bin, workdir, "--config", configPath, "tick")
	if code != 0 || !contains(stdout, "0 job(s) due") {
		t.Fatalf("setup: expected the second tick to find nothing due (exit %d): %s", code, stdout)
	}
	if second := lastTick(); second == first {
		t.Errorf("a tick with nothing due did not update the last tick time (%s)", second)
	}
}

func TestCLI_TickWithInvalidConfigRecordsNothing(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "bad.yaml")
	content := `
backups:
  documents:
    source: {paths: []}
    policy: missing-policy
    repositories: [missing-repo]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runCLI(t, bin, workdir, "--config", configPath, "tick"); code == 0 {
		t.Fatal("expected tick to refuse an invalid config")
	}
	if _, err := os.Stat(filepath.Join(workdir, ".rest-o-matic")); err == nil {
		t.Error("a refused tick wrote to the state directory")
	}
}

func TestCLI_StateWriteFailureIsAWarningNotAJobFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := setupWorkspace(t)
	stateDir := filepath.Join(workdir, ".rest-o-matic")

	// One run creates the state directory and every lock file; after that
	// only the state file itself still needs the directory to be writable.
	if stdout, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents"); code != 0 {
		t.Fatalf("setup run failed (exit %d): %s", code, stdout)
	}
	if err := os.Chmod(stateDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(stateDir, 0o755) })

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	if code != 0 || !contains(stdout, "job documents: OK") {
		t.Errorf("expected the job to still succeed (exit %d): %s", code, stdout)
	}
	if !contains(stderr, "warning: job documents: state could not be saved") {
		t.Errorf("expected a warning that the state could not be saved, got: %s", stderr)
	}
}

// A running marker left by a killed execution is removed by the next one
// to take the job's lock, so that a job which is merely waiting afterwards
// isn't mistaken for a running one.
func TestExecuteWithSlot_ClearsStaleRunningMarker(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas"))
	if err := f.store.MarkRunning("documents", ts(t, "2026-10-01T02:00:00Z"), state.TriggerTick); err != nil {
		t.Fatal(err)
	}

	notDue := func() bool { return false }
	if _, kind := executeWithSlot(context.Background(), context.Background(), cfg, f.store, "documents", f.opts, state.TriggerTick, notDue); kind != jobNotDue {
		t.Fatalf("got kind %v, want jobNotDue", kind)
	}
	st, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if marker := st.Jobs["documents"].Running; marker != nil {
		t.Fatalf("the stale running marker is still there: %+v", marker)
	}
}

// While a job runs it is marked as running, with how it was started; once
// it has finished the run is recorded with the same trigger.
func TestExecuteWithSlot_MarksRunningThenRecordsTheRun(t *testing.T) {
	f := newConcurrencyFixture(t)
	inAfter, release := filepath.Join(f.dir, "in-after"), filepath.Join(f.dir, "release")
	job := f.job("documents", "nas")
	job.Hooks.After.Always = []string{"touch " + inAfter + "; while [ ! -e " + release + " ]; do sleep 0.05; done"}
	cfg := f.config(2, job)

	done := f.startJob(context.Background(), cfg, "documents")
	waitForFile(t, inAfter)
	st, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if marker := st.Jobs["documents"].Running; marker == nil || marker.Trigger != state.TriggerRun {
		t.Fatalf("got running marker %+v while the job runs, want one started by run", marker)
	}
	if held, err := lock.JobHeld(f.opts.LockDir, "documents"); err != nil || !held {
		t.Fatalf("JobHeld while the job runs: held=%v err=%v, want true", held, err)
	}

	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, done)
	st, err = f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	js := st.Jobs["documents"]
	if js.Running != nil || len(js.Runs) != 1 {
		t.Fatalf("got %+v after the job finished, want no marker and one recorded run", js)
	}
	run := js.Runs[0]
	if run.Trigger != state.TriggerRun || run.Outcome != state.OutcomeFailed || run.Error == "" || !run.Finished.After(run.Started) {
		t.Fatalf("got run %+v, want a failed run started by run, with an error and both times", run)
	}
	if len(run.Repositories) != 1 || run.Repositories[0].Result != state.ResultBackupFailed {
		t.Fatalf("got repositories %+v, want nas recorded as a failed backup", run.Repositories)
	}
}
