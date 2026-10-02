//go:build !windows

package execution

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// notifyRunner runs one job, documents, over and over under a clock the
// test controls, feeding each run's failing state into the next as the
// caller of RunJob does.
type notifyRunner struct {
	t     *testing.T
	log   string
	fail  string // the job fails while this file exists
	cfg   *config.Config
	clock time.Time
	prior Prior
}

var notifyStart = time.Date(2026, 1, 5, 2, 0, 0, 0, time.UTC) // a Monday

// newNotifyRunner builds a job whose before hook fails while a marker file
// exists, with every notification kind logging its name.
func newNotifyRunner(t *testing.T) *notifyRunner {
	t.Helper()
	dir := t.TempDir()
	r := &notifyRunner{t: t, log: filepath.Join(dir, "log"), fail: filepath.Join(dir, "fail"), clock: notifyStart}
	r.cfg = hookJob(t, config.Hooks{
		Before: []string{"[ ! -e " + r.fail + " ]"},
		After: config.AfterHooks{
			Success: []string{logHook(r.log, "job-success")},
			Failure: []string{logHook(r.log, "job-failure")},
		},
	}, nil)
	r.cfg.Notify = config.Notify{
		Failure:  []string{logHook(r.log, "FAILURE")},
		Recovery: []string{logHook(r.log, "RECOVERY")},
		Success:  []string{logHook(r.log, "SUCCESS")},
	}
	return r
}

func (r *notifyRunner) setFailing(failing bool) {
	r.t.Helper()
	cmd := "rm -f " + r.fail
	if failing {
		cmd = "touch " + r.fail
	}
	if err := runHook(context.Background(), cmd, nil); err != nil {
		r.t.Fatal(err)
	}
}

// run advances the clock, runs the job, and carries its state forward.
func (r *notifyRunner) run(after time.Duration) JobResult {
	r.t.Helper()
	r.clock = r.clock.Add(after)
	result := RunJob(context.Background(), context.Background(), r.cfg, "documents", Options{
		Restic: NewResticRunner(),
		Prior:  r.prior,
		Now:    func() time.Time { return r.clock },
	})
	r.prior = Prior{Failed: !result.Success(), LastRun: r.clock, FailingSince: result.FailingSince, FailureNotified: result.FailureNotified}
	return result
}

// logged returns what ran since the last call, and clears the log.
func (r *notifyRunner) logged() string {
	r.t.Helper()
	got := readLog(r.t, r.log)
	if err := runHook(context.Background(), "rm -f "+r.log, nil); err != nil {
		r.t.Fatal(err)
	}
	return got
}

func TestNotify_FailureOnceADayThenRecovery(t *testing.T) {
	r := newNotifyRunner(t)
	r.setFailing(true)

	r.run(0)
	if got := r.logged(); got != "job-failure FAILURE" {
		t.Fatalf("first failure: got %q, want the job's hook then the notification", got)
	}
	for i := 0; i < 3; i++ {
		r.run(time.Hour)
		if got := r.logged(); got != "job-failure" {
			t.Fatalf("failure %d within a day: got %q, want the job's own hook only", i+2, got)
		}
	}

	// 23 hours after the notification: still quiet. An hour later: again.
	r.run(20 * time.Hour)
	if got := r.logged(); got != "job-failure" {
		t.Fatalf("23 hours on: got %q, want no notification yet", got)
	}
	r.run(time.Hour)
	if got := r.logged(); got != "job-failure FAILURE" {
		t.Fatalf("24 hours on: got %q, want a second notification", got)
	}

	r.setFailing(false)
	r.run(time.Hour)
	if got := r.logged(); got != "job-success RECOVERY SUCCESS" {
		t.Fatalf("first success: got %q, want the job's hook, then recovery, then success", got)
	}
	r.run(time.Hour)
	if got := r.logged(); got != "job-success SUCCESS" {
		t.Fatalf("second success: got %q, want no recovery", got)
	}
}

func TestNotify_NewRunOfFailuresNotifiesAgain(t *testing.T) {
	r := newNotifyRunner(t)
	r.setFailing(true)
	r.run(0)
	r.setFailing(false)
	r.run(time.Hour)
	r.logged()

	// Broken again within a day of the first alert: a new event.
	r.setFailing(true)
	result := r.run(time.Hour)
	if got := r.logged(); got != "job-failure FAILURE" {
		t.Fatalf("got %q, want a notification for the new run of failures", got)
	}
	if want := notifyStart.Add(2 * time.Hour); result.FailingSince == nil || !result.FailingSince.Equal(want) {
		t.Fatalf("got failing since %v, want the start of the new run of failures, %v", result.FailingSince, want)
	}
}

func TestNotify_FirstEverRun(t *testing.T) {
	t.Run("fails", func(t *testing.T) {
		r := newNotifyRunner(t)
		r.setFailing(true)
		r.run(0)
		if got := r.logged(); got != "job-failure FAILURE" {
			t.Fatalf("got %q, want a notification", got)
		}
	})
	t.Run("succeeds", func(t *testing.T) {
		r := newNotifyRunner(t)
		r.run(0)
		if got := r.logged(); got != "job-success SUCCESS" {
			t.Fatalf("got %q, want success only, with no recovery", got)
		}
	})
}

func TestNotify_UnsentFailureIsRetriedOnTheNextFailure(t *testing.T) {
	r := newNotifyRunner(t)
	r.cfg.Notify.Failure = []string{logHook(r.log, "FAILURE") + "; exit 1"}
	r.setFailing(true)

	result := r.run(0)
	if len(result.NotifyErrs) != 1 || result.FailureNotified != nil {
		t.Fatalf("got NotifyErrs=%v FailureNotified=%v, want one error and the notification not counted as sent", result.NotifyErrs, result.FailureNotified)
	}
	r.logged()

	r.cfg.Notify.Failure = []string{logHook(r.log, "FAILURE")}
	result = r.run(time.Hour)
	if got := r.logged(); got != "job-failure FAILURE" {
		t.Fatalf("got %q, want the notification retried an hour later", got)
	}
	if result.FailureNotified == nil || !result.FailureNotified.Equal(r.clock) {
		t.Fatalf("got FailureNotified=%v, want it set to now once the notification went out", result.FailureNotified)
	}
}

func TestNotify_NeverChangesTheOutcome(t *testing.T) {
	r := newNotifyRunner(t)
	r.cfg.Notify.Success = []string{"exit 1", logHook(r.log, "SUCCESS")}

	result := r.run(0)
	if !result.Success() {
		t.Fatal("a failing notification command changed the job's outcome")
	}
	if len(result.NotifyErrs) != 1 {
		t.Fatalf("got %d notification errors, want the one failure reported", len(result.NotifyErrs))
	}
	if got := r.logged(); got != "job-success SUCCESS" {
		t.Fatalf("got %q, want the command after the failing one to have run", got)
	}
}

func TestNotify_Environment(t *testing.T) {
	r := newNotifyRunner(t)
	dump := func(name string) string {
		return `echo "` + name + `|$RESTOMATIC_JOB|$RESTOMATIC_OUTCOME|$RESTOMATIC_FAILING_SINCE|${RESTOMATIC_ERROR:+has-error}" >> ` + r.log
	}
	r.cfg.Notify = config.Notify{Failure: []string{dump("F")}, Recovery: []string{dump("R")}, Success: []string{dump("S")}}
	r.cfg.Backups["documents"] = func() config.Job { j := r.cfg.Backups["documents"]; j.Hooks.After = config.AfterHooks{}; return j }()

	r.setFailing(true)
	r.run(0)
	if got := r.logged(); got != "F|documents|failure|2026-01-05T02:00:00Z|has-error" {
		t.Fatalf("failure command saw %q", got)
	}

	// A day later the notification repeats, still dated from the first failure.
	r.run(24 * time.Hour)
	if got := r.logged(); got != "F|documents|failure|2026-01-05T02:00:00Z|has-error" {
		t.Fatalf("repeated failure command saw %q, want failing-since unchanged", got)
	}

	r.setFailing(false)
	r.run(time.Hour)
	if got := r.logged(); got != "R|documents|success|2026-01-05T02:00:00Z| S|documents|success||" {
		t.Fatalf("recovery and success commands saw %q", got)
	}
}

// A job that was already failing under a version that didn't track this
// has only its last outcome and run time.
func TestNotify_JobAlreadyFailingBeforeUpgrade(t *testing.T) {
	r := newNotifyRunner(t)
	earlier := notifyStart.Add(-48 * time.Hour)
	r.prior = Prior{Failed: true, LastRun: earlier}

	r.setFailing(true)
	result := r.run(0)
	if got := r.logged(); got != "job-failure FAILURE" {
		t.Fatalf("got %q, want a notification for a job with none recorded", got)
	}
	if result.FailingSince == nil || !result.FailingSince.Equal(earlier) {
		t.Fatalf("got failing since %v, want the last known failed run, %v", result.FailingSince, earlier)
	}

	r.setFailing(false)
	r.run(time.Hour)
	if got := r.logged(); got != "job-success RECOVERY SUCCESS" {
		t.Fatalf("got %q, want a recovery", got)
	}
}

func TestNotify_NothingConfigured(t *testing.T) {
	r := newNotifyRunner(t)
	r.cfg.Notify = config.Notify{}
	r.setFailing(true)

	result := r.run(0)
	if got := r.logged(); got != "job-failure" {
		t.Fatalf("got %q, want only the job's own hook", got)
	}
	// Failing-since is still tracked, for status.
	if result.FailingSince == nil || result.FailureNotified != nil {
		t.Fatalf("got FailingSince=%v FailureNotified=%v, want a failing-since time and no notification", result.FailingSince, result.FailureNotified)
	}
}

func TestNotify_InterruptedRunNotifiesAsFailureWithinTheCleanupLimit(t *testing.T) {
	old := cleanupGrace
	cleanupGrace = 300 * time.Millisecond
	t.Cleanup(func() { cleanupGrace = old })

	r := newNotifyRunner(t)
	r.cfg.Notify.Failure = []string{logHook(r.log, "FAILURE"), "sleep 30"}

	work, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("interrupted by SIGTERM"))
	start := time.Now()
	result := RunJob(work, context.Background(), r.cfg, "documents", Options{Restic: NewResticRunner(), Now: func() time.Time { return notifyStart }})

	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("a hung notification command ran for %v, want it stopped by the cleanup limit", elapsed)
	}
	if got := r.logged(); got != "job-failure FAILURE" {
		t.Fatalf("got %q, want the failure notification to have run", got)
	}
	if result.FailureNotified != nil {
		t.Fatal("a notification whose command was stopped must not count as sent")
	}
}
