//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// concurrencyFixture is a state directory plus a hook log, for tests that
// call executeWithSlot and dispatch in-process.
type concurrencyFixture struct {
	dir   string
	log   string
	store *state.Store
	opts  execution.Options
}

func newConcurrencyFixture(t *testing.T) *concurrencyFixture {
	t.Helper()
	dir := t.TempDir()
	return &concurrencyFixture{
		dir:   dir,
		log:   filepath.Join(dir, "hooks.log"),
		store: state.NewStore(filepath.Join(dir, "state", "state.json"), filepath.Join(dir, "state", "locks")),
		opts:  execution.Options{Restic: execution.NewResticRunner(), LockDir: filepath.Join(dir, "state", "locks")},
	}
}

// job returns a job that logs "start <name>" and "end <name>" around its
// backup, so tests can see whether and in what order hooks ran.
func (f *concurrencyFixture) job(name string, repos ...string) config.Job {
	job := config.Job{
		Name:   name,
		Source: config.Source{Paths: []string{f.dir}},
		Hooks: config.Hooks{
			Before: []string{"echo start " + name + " >> " + f.log},
			After:  config.AfterHooks{Always: []string{"echo end " + name + " >> " + f.log}},
		},
		Policy: "hot",
	}
	for _, r := range repos {
		job.Repositories = append(job.Repositories, config.RepositoryRef{Name: r})
	}
	return job
}

// config builds a config whose repositories were never initialised, so
// every backup fails quickly; these tests are about what runs and when,
// not about restic.
func (f *concurrencyFixture) config(maxConcurrent int, jobs ...config.Job) *config.Config {
	cfg := &config.Config{
		MaxConcurrent: maxConcurrent,
		Policies:      map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 24}}},
		Repositories:  map[string]config.Repository{},
		Backups:       map[string]config.Job{},
	}
	for _, job := range jobs {
		cfg.Backups[job.Name] = job
		for _, ref := range job.Repositories {
			cfg.Repositories[ref.Name] = config.Repository{
				Backend:  "local",
				URL:      filepath.Join(f.dir, "never-initialised-"+ref.Name),
				Password: config.Plain("x"),
			}
		}
	}
	return cfg
}

func (f *concurrencyFixture) hookLog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(data)), " ")
}

func (f *concurrencyFixture) recorded(t *testing.T, job string) bool {
	t.Helper()
	st, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	return len(st.Jobs[job].Runs) > 0
}

type started struct {
	result execution.JobResult
	kind   startKind
}

// startJob runs executeWithSlot in the background.
func (f *concurrencyFixture) startJob(work context.Context, cfg *config.Config, name string) <-chan started {
	done := make(chan started, 1)
	go func() {
		result, kind := executeWithSlot(work, context.Background(), cfg, f.store, name, f.opts, state.TriggerRun, nil)
		done <- started{result, kind}
	}()
	return done
}

func waitStarted(t *testing.T, done <-chan started) started {
	t.Helper()
	select {
	case s := <-done:
		return s
	case <-time.After(15 * time.Second):
		t.Fatal("job did not finish")
		return started{}
	}
}

// assertStillWaiting fails if the job finishes, or any hook runs, while
// what it needs is held.
func (f *concurrencyFixture) assertStillWaiting(t *testing.T, done <-chan started) {
	t.Helper()
	select {
	case s := <-done:
		t.Fatalf("job finished while it should have been waiting: kind=%v result=%+v", s.kind, s.result)
	case <-time.After(300 * time.Millisecond):
	}
	if log := f.hookLog(t); log != "" {
		t.Fatalf("hooks ran while the job should have been waiting: %q", log)
	}
}

func TestExecuteWithSlot_WaitsForRepositoryBeforeRunningHooks(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas"))

	held, ok, err := lock.AcquireRepository(f.opts.LockDir, "nas")
	if err != nil || !ok {
		t.Fatalf("holding nas: ok=%v err=%v", ok, err)
	}

	done := f.startJob(context.Background(), cfg, "documents")
	f.assertStillWaiting(t, done)

	if err := held.Unlock(); err != nil {
		t.Fatal(err)
	}
	if s := waitStarted(t, done); s.kind != jobRan {
		t.Fatalf("got kind %v, want jobRan", s.kind)
	}
	if got := f.hookLog(t); got != "start documents end documents" {
		t.Fatalf("got hook log %q, want the job to have run once the repository was free", got)
	}
}

func TestExecuteWithSlot_WaitsForEveryRepositoryBeforeStarting(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas", "offsite"))

	held, ok, err := lock.AcquireRepository(f.opts.LockDir, "offsite")
	if err != nil || !ok {
		t.Fatalf("holding offsite: ok=%v err=%v", ok, err)
	}

	done := f.startJob(context.Background(), cfg, "documents")
	f.assertStillWaiting(t, done)

	if err := held.Unlock(); err != nil {
		t.Fatal(err)
	}
	s := waitStarted(t, done)
	if s.kind != jobRan || len(s.result.Repos) != 2 {
		t.Fatalf("got kind %v with %d repositories attempted, want jobRan with 2", s.kind, len(s.result.Repos))
	}
}

func TestExecuteWithSlot_InterruptedWhileWaitingIsNotStarted(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas"))

	held, ok, err := lock.AcquireRepository(f.opts.LockDir, "nas")
	if err != nil || !ok {
		t.Fatalf("holding nas: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	work, cancel := context.WithCancelCause(context.Background())
	done := f.startJob(work, cfg, "documents")
	f.assertStillWaiting(t, done)
	cancel(errors.New("interrupted by SIGTERM"))

	if s := waitStarted(t, done); s.kind != jobNotStarted {
		t.Fatalf("got kind %v, want jobNotStarted", s.kind)
	}
	if log := f.hookLog(t); log != "" {
		t.Fatalf("hooks ran for a job interrupted while waiting: %q", log)
	}
	if f.recorded(t, "documents") {
		t.Fatal("a run was recorded for a job interrupted while waiting")
	}
}

func TestExecuteWithSlot_WaitsForSlotThenReportsRealOutcome(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(1, f.job("documents", "nas"))

	held, ok, err := lock.AcquireSlot(f.opts.LockDir, 1)
	if err != nil || !ok {
		t.Fatalf("holding the only slot: ok=%v err=%v", ok, err)
	}

	done := f.startJob(context.Background(), cfg, "documents")
	f.assertStillWaiting(t, done)

	if err := held.Unlock(); err != nil {
		t.Fatal(err)
	}
	s := waitStarted(t, done)
	if s.kind != jobRan {
		t.Fatalf("got kind %v, want jobRan", s.kind)
	}
	// The job's own result, not a "no slot" failure: hooks ran and the
	// repository was attempted.
	if s.result.HookErr != nil || len(s.result.Repos) != 1 {
		t.Fatalf("got result %+v, want the job to have run against its repository", s.result)
	}
}

func TestExecuteWithSlot_AlreadyRunningJobIsNotStarted(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas"))

	held, ok, err := lock.AcquireJob(f.opts.LockDir, "documents")
	if err != nil || !ok {
		t.Fatalf("holding the job lock: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	if s := waitStarted(t, f.startJob(context.Background(), cfg, "documents")); s.kind != jobAlreadyRunning {
		t.Fatalf("got kind %v, want jobAlreadyRunning", s.kind)
	}
	if log := f.hookLog(t); log != "" {
		t.Fatalf("hooks ran for an already-running job: %q", log)
	}
	if f.recorded(t, "documents") {
		t.Fatal("a run was recorded for an already-running job")
	}
}

func TestExecuteWithSlot_RepositoryListedTwiceDoesNotWaitOnItself(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas", "nas"))

	if s := waitStarted(t, f.startJob(context.Background(), cfg, "documents")); s.kind != jobRan {
		t.Fatalf("got kind %v, want jobRan", s.kind)
	}
}

// The job lock must outlive the after hooks and the state write: released
// any earlier, a tick landing in the gap would start the job again.
func TestExecuteWithSlot_HoldsJobLockUntilRecorded(t *testing.T) {
	f := newConcurrencyFixture(t)
	inAfter, release := filepath.Join(f.dir, "in-after"), filepath.Join(f.dir, "release")
	job := f.job("documents", "nas")
	job.Hooks.After.Always = []string{"touch " + inAfter + "; while [ ! -e " + release + " ]; do sleep 0.05; done"}
	cfg := f.config(2, job)

	done := f.startJob(context.Background(), cfg, "documents")
	waitForFile(t, inAfter)
	if _, ok, err := lock.AcquireJob(f.opts.LockDir, "documents"); err != nil || ok {
		t.Fatalf("job lock during the after hooks: ok=%v err=%v, want it held", ok, err)
	}

	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, done)
	if !f.recorded(t, "documents") {
		t.Fatal("expected the run to be recorded")
	}
	l, ok, err := lock.AcquireJob(f.opts.LockDir, "documents")
	if err != nil || !ok {
		t.Fatalf("job lock after the job finished: ok=%v err=%v, want it free", ok, err)
	}
	l.Unlock()
}

func TestDispatch_SkipsRunningJobAndRunsTheOther(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas"), f.job("gitea", "offsite"))

	held, ok, err := lock.AcquireJob(f.opts.LockDir, "documents")
	if err != nil || !ok {
		t.Fatalf("holding the job lock: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	attempts := dispatch(context.Background(), context.Background(), cfg, f.store, []string{"documents", "gitea"}, f.opts)
	if attempts[0].kind != jobAlreadyRunning || attempts[1].kind != jobRan {
		t.Fatalf("got kinds %v and %v, want jobAlreadyRunning and jobRan", attempts[0].kind, attempts[1].kind)
	}
	if got := f.hookLog(t); got != "start gitea end gitea" {
		t.Fatalf("got hook log %q, want only gitea's hooks", got)
	}
	if f.recorded(t, "documents") || !f.recorded(t, "gitea") {
		t.Fatal("expected a recorded run for gitea only")
	}
}

// A job another execution ran after the due list was worked out must not
// run again for the same schedule boundary.
func TestDispatch_DoesNotRepeatJobRunMeanwhile(t *testing.T) {
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas"))

	if err := f.store.RecordRun("documents", state.RunRecord{Finished: time.Now(), Outcome: state.OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}

	attempts := dispatch(context.Background(), context.Background(), cfg, f.store, []string{"documents"}, f.opts)
	if attempts[0].kind != jobNotDue {
		t.Fatalf("got kind %v, want jobNotDue", attempts[0].kind)
	}
	if log := f.hookLog(t); log != "" {
		t.Fatalf("hooks ran for a job that was no longer due: %q", log)
	}
}

// Two jobs on one repository, due together: the second waits for the first
// and then runs, instead of being recorded as failed and skipped until its
// next schedule boundary.
func TestDispatch_JobsSharingARepositoryBothBackUp(t *testing.T) {
	requireRestic(t)
	f := newConcurrencyFixture(t)
	cfg := f.config(2, f.job("documents", "nas"), f.job("postgres", "nas"))

	repo := cfg.Repositories["nas"]
	initCmd := exec.Command("restic", "-r", repo.URL, "init")
	initCmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+repo.Password.Value)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("restic init: %v: %s", err, out)
	}

	attempts := dispatch(context.Background(), context.Background(), cfg, f.store, []string{"documents", "postgres"}, f.opts)
	for _, a := range attempts {
		if a.kind != jobRan || !a.result.Success() {
			t.Fatalf("job %s: kind=%v result=%+v, want a successful run", a.result.Job, a.kind, a.result)
		}
	}

	// One job's hooks never run inside the other's.
	log := f.hookLog(t)
	if log != "start documents end documents start postgres end postgres" &&
		log != "start postgres end postgres start documents end documents" {
		t.Fatalf("got hook log %q, want the two jobs one after the other", log)
	}

	st, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"documents", "postgres"} {
		js := st.Jobs[name]
		if js.LastOutcome != "success" {
			t.Errorf("job %s recorded as %q, want success", name, js.LastOutcome)
		}
		if len(js.Runs) != 1 || js.Runs[0].Trigger != state.TriggerTick || js.Runs[0].Repositories[0].SnapshotID == "" {
			t.Errorf("job %s: got runs %+v, want one run started by tick with its snapshot ID", name, js.Runs)
		}
	}

	snapCmd := exec.Command("restic", "-r", repo.URL, "snapshots", "--json")
	snapCmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+repo.Password.Value)
	out, err := snapCmd.Output()
	if err != nil {
		t.Fatalf("restic snapshots: %v", err)
	}
	for _, name := range []string{"documents", "postgres"} {
		if !strings.Contains(string(out), `"`+name+`"`) {
			t.Errorf("no snapshot tagged %s in the repository: %s", name, out)
		}
	}
}

// holdJobLock holds a job's lock in the workspace's default state
// directory, as a running execution in another process would.
func holdJobLock(t *testing.T, workdir, job string) {
	t.Helper()
	held, ok, err := lock.AcquireJob(filepath.Join(workdir, ".rest-o-matic", "locks"), job)
	if err != nil || !ok {
		t.Fatalf("holding the job lock: ok=%v err=%v", ok, err)
	}
	t.Cleanup(func() { held.Unlock() })
}

func writeHookedConfig(t *testing.T, workdir, marker string) string {
	t.Helper()
	return writeSignalConfig(t, workdir, `
  documents:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
    hooks:
      before: ["touch `+marker+`"]
      after:
        always: ["touch `+marker+`"]
        failure: ["touch `+marker+`"]
`)
}

func TestCLI_TickSkipsAlreadyRunningJobWithoutFailing(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	marker := filepath.Join(workdir, "hook-ran")
	configPath := writeHookedConfig(t, workdir, marker)
	holdJobLock(t, workdir, "documents")

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "tick")
	if code != 0 {
		t.Fatalf("expected exit 0 when the only due job is already running, got %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !contains(stdout, "job documents: already running, skipped") {
		t.Errorf("expected the job to be reported as already running, got: %s", stdout)
	}
	if !contains(stdout, "1 job(s) due, 0 succeeded, 0 failed, 1 already running") {
		t.Errorf("expected the summary to count the running job, got: %s", stdout)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a hook ran for an already-running job")
	}
	// The tick records its own time, but nothing for the job.
	st, _ := os.ReadFile(filepath.Join(workdir, ".rest-o-matic", "state.json"))
	if contains(string(st), "documents") {
		t.Errorf("state was written for an already-running job: %s", st)
	}
}

func TestCLI_RunRefusesAlreadyRunningJob(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	marker := filepath.Join(workdir, "hook-ran")
	configPath := writeHookedConfig(t, workdir, marker)
	holdJobLock(t, workdir, "documents")

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	if code == 0 {
		t.Fatal("expected a non-zero exit for a job that is already running")
	}
	if !contains(stderr, `job "documents" is already running`) {
		t.Errorf("expected stderr to say the job is already running, got: %s", stderr)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a hook ran for an already-running job")
	}
}
