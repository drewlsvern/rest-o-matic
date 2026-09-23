package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// hookJob builds a single-job config whose hooks are given and which targets
// the given repositories (none, for tests that only exercise hooks).
func hookJob(t *testing.T, hooks config.Hooks, repos map[string]config.Repository) *config.Config {
	t.Helper()
	var refs []config.RepositoryRef
	for name := range repos {
		refs = append(refs, config.RepositoryRef{Name: name})
	}
	job := config.Job{
		Name:         "documents",
		Source:       config.Source{Paths: []string{t.TempDir()}},
		Hooks:        hooks,
		Policy:       "hot",
		Repositories: refs,
	}
	return testConfig(t, "documents", job, repos)
}

// logHook returns a command appending name to log, so tests can check which
// hooks ran and in what order.
func logHook(log, name string) string { return "echo " + name + " >> " + log }

func readLog(t *testing.T, log string) string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

func runJob(cfg *config.Config, lockDir string) JobResult {
	return RunJob(context.Background(), context.Background(), cfg, "documents", Options{Restic: NewResticRunner(), LockDir: lockDir})
}

func TestRunJob_SuccessRunsAlwaysThenSuccess(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	cfg := hookJob(t, config.Hooks{
		Before: []string{logHook(log, "before")},
		After: config.AfterHooks{
			Always:  []string{logHook(log, "always")},
			Success: []string{logHook(log, "success")},
			Failure: []string{logHook(log, "failure")},
		},
	}, nil)

	if r := runJob(cfg, t.TempDir()); !r.Success() {
		t.Fatalf("expected success, got %+v", r)
	}
	if got := readLog(t, log); got != "before always success" {
		t.Fatalf("got hook order %q, want %q", got, "before always success")
	}
}

func TestRunJob_AlwaysRunsBeforeFailureWhateverTheKeyOrder(t *testing.T) {
	requireRestic(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	path := filepath.Join(dir, "cfg.yaml")
	yaml := `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  bad: {backend: local, url: ` + filepath.Join(dir, "never-initialized") + `, password: x}
backups:
  documents:
    source: {paths: ["` + dir + `"]}
    policy: hot
    repositories: [bad]
    hooks:
      after:
        failure: ["` + logHook(log, "failure") + `"]
        always: ["` + logHook(log, "always") + `"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if r := runJob(cfg, t.TempDir()); r.Success() {
		t.Fatal("expected the uninitialized repository to fail the job")
	}
	if got := readLog(t, log); got != "always failure" {
		t.Fatalf("got hook order %q, want %q", got, "always failure")
	}
}

func TestRunJob_BeforeFailureRunsAlwaysThenFailure(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	cfg := hookJob(t, config.Hooks{
		Before: []string{"exit 1", logHook(log, "before2")},
		After: config.AfterHooks{
			Always:  []string{logHook(log, "always")},
			Success: []string{logHook(log, "success")},
			Failure: []string{logHook(log, "failure")},
		},
	}, nil)

	r := runJob(cfg, t.TempDir())
	if r.Success() || r.HookErr == nil {
		t.Fatalf("expected a before-hook failure, got %+v", r)
	}
	if got := readLog(t, log); got != "always failure" {
		t.Fatalf("got hook order %q, want %q", got, "always failure")
	}
}

func TestRunJob_FailedAlwaysFailsJob(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	cfg := hookJob(t, config.Hooks{
		After: config.AfterHooks{
			Always:  []string{"exit 1"},
			Success: []string{logHook(log, "success")},
			Failure: []string{logHook(log, "failure")},
		},
	}, nil)

	r := runJob(cfg, t.TempDir())
	if r.Success() || len(r.AlwaysErrs) != 1 {
		t.Fatalf("expected the always failure to fail the job, got %+v", r)
	}
	if got := readLog(t, log); got != "failure" {
		t.Fatalf("expected only the failure hook to run, got %q", got)
	}
}

func TestRunJob_FailingSuccessHookKeepsOutcome(t *testing.T) {
	cfg := hookJob(t, config.Hooks{
		After: config.AfterHooks{Success: []string{"exit 1"}},
	}, nil)

	r := runJob(cfg, t.TempDir())
	if !r.Success() {
		t.Fatalf("expected a failing success hook not to change the outcome, got %+v", r)
	}
	if len(r.OutcomeHookErrs) != 1 {
		t.Fatalf("expected the success hook failure to be reported, got %v", r.OutcomeHookErrs)
	}
}

func TestRunJob_AlwaysRunsEveryCommand(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	cfg := hookJob(t, config.Hooks{
		After: config.AfterHooks{Always: []string{"exit 1", logHook(log, "second")}},
	}, nil)

	r := runJob(cfg, t.TempDir())
	if len(r.AlwaysErrs) != 1 {
		t.Fatalf("expected one always error, got %v", r.AlwaysErrs)
	}
	if got := readLog(t, log); got != "second" {
		t.Fatalf("expected the second always command to run, got %q", got)
	}
}

func TestRunJob_HookEnvironment(t *testing.T) {
	requireRestic(t)
	dir := t.TempDir()
	envOut := filepath.Join(dir, "env")
	bad := config.Repository{Backend: "local", URL: filepath.Join(dir, "never-initialized"), Password: "x"}
	cfg := hookJob(t, config.Hooks{
		Before: []string{`echo "before=$RESTOMATIC_JOB" >> ` + envOut},
		After: config.AfterHooks{Failure: []string{
			`printf 'job=%s\noutcome=%s\nfailed=%s\nerror=%s\n' "$RESTOMATIC_JOB" "$RESTOMATIC_OUTCOME" "$RESTOMATIC_FAILED_REPOS" "$RESTOMATIC_ERROR" >> ` + envOut,
		}},
	}, map[string]config.Repository{"bad": bad})

	runJob(cfg, t.TempDir())

	b, err := os.ReadFile(envOut)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"before=documents\n", "job=documents\n", "outcome=failure\n", "failed=bad\n", "error=repository bad: "} {
		if !strings.Contains(got, want) {
			t.Errorf("expected hook env output to contain %q, got:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "error=") && len(line) > len("error=")+maxErrorEnvLen {
			t.Errorf("RESTOMATIC_ERROR exceeds %d bytes: %d", maxErrorEnvLen, len(line)-len("error="))
		}
	}
}

func TestRunJob_InterruptRunsCleanupAndFailure(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	envOut := filepath.Join(t.TempDir(), "env")
	cfg := hookJob(t, config.Hooks{
		Before: []string{"sleep 30"},
		After: config.AfterHooks{
			Always:  []string{logHook(log, "always")},
			Success: []string{logHook(log, "success")},
			Failure: []string{logHook(log, "failure"), `echo "$RESTOMATIC_ERROR" > ` + envOut},
		},
	}, nil)

	work, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(200*time.Millisecond, func() { cancel(errors.New("interrupted by SIGTERM")) })

	start := time.Now()
	r := RunJob(work, context.Background(), cfg, "documents", Options{Restic: NewResticRunner(), LockDir: t.TempDir()})
	if time.Since(start) > 5*time.Second {
		t.Fatalf("interrupt did not stop the before hook promptly (%v)", time.Since(start))
	}
	if !r.Interrupted || r.Success() {
		t.Fatalf("expected an interrupted, failed job, got %+v", r)
	}
	if got := readLog(t, log); got != "always failure" {
		t.Fatalf("got hook order %q, want %q", got, "always failure")
	}
	if b, _ := os.ReadFile(envOut); !strings.Contains(string(b), "interrupted by SIGTERM") {
		t.Fatalf("expected RESTOMATIC_ERROR to describe the interrupt, got %q", b)
	}
}

func TestRunJob_CleanupBoundedAfterInterrupt(t *testing.T) {
	orig := cleanupGrace
	cleanupGrace = 300 * time.Millisecond
	t.Cleanup(func() { cleanupGrace = orig })

	cfg := hookJob(t, config.Hooks{
		After: config.AfterHooks{Always: []string{"sleep 30"}},
	}, nil)
	work, cancel := context.WithCancel(context.Background())
	cancel() // already interrupted when the after hooks start

	start := time.Now()
	r := RunJob(work, context.Background(), cfg, "documents", Options{Restic: NewResticRunner(), LockDir: t.TempDir()})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hung always hook was not stopped after the grace period (%v)", elapsed)
	}
	if len(r.AlwaysErrs) != 1 {
		t.Fatalf("expected the stopped always hook to be reported, got %v", r.AlwaysErrs)
	}
}
