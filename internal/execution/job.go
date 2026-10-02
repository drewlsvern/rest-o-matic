package execution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// Options bundles what RunJob needs beyond the config and job name.
type Options struct {
	Restic  *ResticRunner
	LockDir string
	// StateDir, when set, is checked for ownership by exec before it
	// takes a repository lock. run and tick check it themselves, once,
	// before anything starts.
	StateDir string

	// Prior is what was recorded about the job before this run. It decides
	// which notifications apply; the zero value is a job that never ran.
	Prior Prior
	// Now is the clock; nil means time.Now. Tests set it.
	Now func() time.Time
}

// Prior is a job's recorded state going into a run.
type Prior struct {
	// Failed is whether the previous run failed.
	Failed bool
	// LastRun is when the previous run finished; zero if there was none.
	LastRun time.Time
	// FailingSince and FailureNotified are as recorded with that run. Both
	// may be nil even when Failed is set, for a state written by a version
	// that didn't track them.
	FailingSince    *time.Time
	FailureNotified *time.Time
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// failureNotifyInterval is the least time between failure notifications
// for a job that keeps failing.
const failureNotifyInterval = 24 * time.Hour

// RepoOutcome is the result of attempting one job's backup+forget against
// one repository.
type RepoOutcome struct {
	Repository string
	// SnapshotID is the snapshot the backup created; empty if it failed.
	SnapshotID string
	BackupErr  error
	ForgetErr  error
}

// FailureSummary is a one-line description of what failed against this
// repository, empty if nothing did.
func (o RepoOutcome) FailureSummary() string {
	switch {
	case o.BackupErr != nil:
		return oneLine(o.BackupErr.Error())
	case o.ForgetErr != nil:
		return oneLine(o.ForgetErr.Error())
	}
	return ""
}

func (o RepoOutcome) ok() bool {
	return o.BackupErr == nil && o.ForgetErr == nil
}

// JobResult is the aggregated outcome of one job execution.
type JobResult struct {
	Job     string
	HookErr error // set only on a before-hook failure, which aborts all repository backups
	Repos   []RepoOutcome

	// AlwaysErrs are failures of `after.always` commands. They count toward
	// the job's outcome: a cleanup that failed (e.g. a container that didn't
	// restart) is a failed job.
	AlwaysErrs []error
	// OutcomeHookErrs are failures of the `after.success` or `after.failure`
	// commands. They are reported but never change the outcome.
	OutcomeHookErrs []error
	// Interrupted is set when a signal stopped the job before its backups
	// finished; InterruptErr says which signal.
	Interrupted  bool
	InterruptErr error

	// NotifyErrs are failures of notification commands. Like
	// OutcomeHookErrs they are reported but never change the outcome.
	NotifyErrs []error
	// FailingSince is set when the job failed: when the first run of the
	// current run of failures started. FailureNotified is when the failure
	// notification was last sent during it, nil if it hasn't been. The
	// caller records both for the next run's Prior.
	FailingSince    *time.Time
	FailureNotified *time.Time
}

// Success reports whether the job's before hooks, every repository it
// targeted, and its always hooks all succeeded without interruption.
func (r JobResult) Success() bool {
	if r.HookErr != nil || r.Interrupted || len(r.AlwaysErrs) > 0 {
		return false
	}
	for _, ro := range r.Repos {
		if !ro.ok() {
			return false
		}
	}
	return true
}

// cleanupGrace bounds how long the after hooks may run once the job has
// been interrupted: long enough for a container restart, short enough to
// finish inside systemd's default 90s stop timeout. A variable so tests can
// shorten it.
var cleanupGrace = 60 * time.Second

// maxErrorEnvLen caps RESTOMATIC_ERROR so restic's verbose stderr can't
// produce an unwieldy environment variable.
const maxErrorEnvLen = 500

// RunJob executes a single job end-to-end:
//  1. before hooks (once; the first failure stops them and aborts all
//     repository backups)
//  2. an independent backup+forget attempt against each configured
//     repository
//  3. after.always hooks (once, on every outcome)
//  4. after.success or after.failure hooks (once, depending on the outcome
//     including step 3)
//  5. the config's notify commands that apply, given the outcome and
//     opts.Prior (see notify)
//
// work stops steps 1-2 when cancelled (the first SIGINT/SIGTERM); the after
// hooks still run under cleanup, which a second signal cancels. Once work
// is cancelled the after hooks are also limited to cleanupGrace.
//
// RunJob takes no locks. The caller must already hold the cross-process
// lock of every repository the job backs up to, so that nothing of the job
// runs until all of it can.
func RunJob(work, cleanup context.Context, cfg *config.Config, jobName string, opts Options) JobResult {
	job := cfg.Backups[jobName]
	result := JobResult{Job: jobName}
	env := []string{"RESTOMATIC_JOB=" + jobName}
	started := opts.now()

	if err := runHooksStopOnError(work, job.Hooks.Before, env); err != nil {
		result.HookErr = err
	} else {
		tags := append([]string{jobName}, job.Tags...)
		// Records how the files were read, so a restore can use the same
		// mode; direct snapshots stay untagged, like every snapshot taken
		// before read modes existed.
		if mode := job.ReadMode(); mode != config.ReadDirect {
			tags = append(tags, readModeTag(mode))
		}
		for _, ref := range job.Repositories {
			if work.Err() != nil {
				break
			}
			result.Repos = append(result.Repos, runRepo(work, cfg, jobName, ref.Name, tags, opts))
		}
	}
	if work.Err() != nil {
		result.Interrupted = true
		result.InterruptErr = context.Cause(work)
	}

	afterCtx, cancel := context.WithCancel(cleanup)
	defer cancel()
	// The grace period starts at the interrupt, whether that came before or
	// during the after hooks.
	stop := context.AfterFunc(work, func() { time.AfterFunc(cleanupGrace, cancel) })
	defer stop()

	result.AlwaysErrs = runHooksAll(afterCtx, job.Hooks.After.Always, env)

	outcomeEnv := append(env, outcomeEnvVars(result)...)
	if result.Success() {
		result.OutcomeHookErrs = runHooksAll(afterCtx, job.Hooks.After.Success, outcomeEnv)
	} else {
		result.OutcomeHookErrs = runHooksAll(afterCtx, job.Hooks.After.Failure, outcomeEnv)
	}

	notify(afterCtx, cfg.Notify, opts, started, outcomeEnv, &result)
	return result
}

// notify runs the notification commands that apply to a finished job and
// records the job's failing state in result.
//
//   - failure: when the job has just started failing, and then only once
//     failureNotifyInterval has passed since it was last sent. It counts as
//     sent only if every command succeeded, so an alert that couldn't be
//     delivered is tried again on the next failed run.
//   - recovery: on a success that follows a failure.
//   - success: on every success.
func notify(ctx context.Context, n config.Notify, opts Options, started time.Time, outcomeEnv []string, result *JobResult) {
	run := func(commands []string, since *time.Time) []error {
		failingSince := ""
		if since != nil {
			failingSince = since.UTC().Format(time.RFC3339)
		}
		return runHooksAll(ctx, commands, append(outcomeEnv[:len(outcomeEnv):len(outcomeEnv)], "RESTOMATIC_FAILING_SINCE="+failingSince))
	}
	prior := opts.Prior

	if result.Success() {
		if prior.Failed {
			result.NotifyErrs = run(n.Recovery, priorFailingSince(prior))
		}
		result.NotifyErrs = append(result.NotifyErrs, run(n.Success, nil)...)
		return
	}

	// A failure following a failure continues that run of failures;
	// otherwise a new one starts with this run.
	since, notified := &started, (*time.Time)(nil)
	if prior.Failed {
		since, notified = priorFailingSince(prior), prior.FailureNotified
	}
	if len(n.Failure) > 0 && (notified == nil || opts.now().Sub(*notified) >= failureNotifyInterval) {
		result.NotifyErrs = run(n.Failure, since)
		notified = nil
		if len(result.NotifyErrs) == 0 {
			now := opts.now()
			notified = &now
		}
	}
	result.FailingSince, result.FailureNotified = since, notified
}

// priorFailingSince is when a job that was already failing began to: the
// recorded time, or its last run for a state that never recorded one.
func priorFailingSince(prior Prior) *time.Time {
	if prior.FailingSince != nil {
		return prior.FailingSince
	}
	lastRun := prior.LastRun
	return &lastRun
}

// outcomeEnvVars describes a finished job to its success/failure hooks.
func outcomeEnvVars(r JobResult) []string {
	outcome := "success"
	if !r.Success() {
		outcome = "failure"
	}
	var failed []string
	for _, ro := range r.Repos {
		if !ro.ok() {
			failed = append(failed, ro.Repository)
		}
	}
	return []string{
		"RESTOMATIC_OUTCOME=" + outcome,
		"RESTOMATIC_FAILED_REPOS=" + strings.Join(failed, ","),
		"RESTOMATIC_ERROR=" + r.FailureSummary(),
	}
}

// FailureSummary is a one-line description of the job's first failure,
// empty on success. It is what hooks receive as RESTOMATIC_ERROR.
func (r JobResult) FailureSummary() string {
	return oneLine(firstFailure(r))
}

// firstFailure returns the message of the most significant failure: the
// interruption, then a before hook, then the first failed repository, then
// the first failed always hook.
func firstFailure(r JobResult) string {
	switch {
	case r.Interrupted && r.InterruptErr != nil:
		return r.InterruptErr.Error()
	case r.HookErr != nil:
		return r.HookErr.Error()
	}
	for _, ro := range r.Repos {
		switch {
		case ro.BackupErr != nil:
			return fmt.Sprintf("repository %s: %v", ro.Repository, ro.BackupErr)
		case ro.ForgetErr != nil:
			return fmt.Sprintf("repository %s: %v", ro.Repository, ro.ForgetErr)
		}
	}
	if len(r.AlwaysErrs) > 0 {
		return r.AlwaysErrs[0].Error()
	}
	return ""
}

// oneLine flattens s onto a single line and caps its length, keeping it
// valid UTF-8.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxErrorEnvLen {
		s = strings.ToValidUTF8(s[:maxErrorEnvLen], "")
	}
	return s
}

func runRepo(ctx context.Context, cfg *config.Config, jobName, repoName string, tags []string, opts Options) RepoOutcome {
	outcome := RepoOutcome{Repository: repoName}

	repo := cfg.Repositories[repoName]
	job := cfg.Backups[jobName]

	snapshotID, err := opts.Restic.Backup(ctx, repo, job.ReadMode(), job.Source.Paths, tags)
	if err != nil {
		outcome.BackupErr = err
		return outcome
	}
	outcome.SnapshotID = snapshotID

	retention, err := cfg.EffectiveRetention(jobName, repoName)
	if err != nil {
		outcome.ForgetErr = err
		return outcome
	}
	if err := opts.Restic.Forget(ctx, repo, jobName, retention); err != nil {
		outcome.ForgetErr = err
	}
	return outcome
}
