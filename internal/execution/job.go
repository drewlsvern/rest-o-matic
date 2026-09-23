package execution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
)

// Options bundles what RunJob needs beyond the config and job name.
type Options struct {
	Restic  *ResticRunner
	LockDir string
}

// RepoOutcome is the result of attempting one job's backup+forget against
// one repository.
type RepoOutcome struct {
	Repository string
	Deferred   bool // true if a lock held by another execution blocked this repo this pass
	BackupErr  error
	ForgetErr  error
}

func (o RepoOutcome) ok() bool {
	return !o.Deferred && o.BackupErr == nil && o.ForgetErr == nil
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
//     repository, each guarded by that repository's cross-process lock
//  3. after.always hooks (once, on every outcome)
//  4. after.success or after.failure hooks (once, depending on the outcome
//     including step 3)
//
// work stops steps 1-2 when cancelled (the first SIGINT/SIGTERM); the after
// hooks still run under cleanup, which a second signal cancels. Once work
// is cancelled the after hooks are also limited to cleanupGrace.
func RunJob(work, cleanup context.Context, cfg *config.Config, jobName string, opts Options) JobResult {
	job := cfg.Backups[jobName]
	result := JobResult{Job: jobName}
	env := []string{"RESTOMATIC_JOB=" + jobName}

	if err := runHooksStopOnError(work, job.Hooks.Before, env); err != nil {
		result.HookErr = err
	} else {
		tags := append([]string{jobName}, job.Tags...)
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
	return result
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
		"RESTOMATIC_ERROR=" + oneLine(firstFailure(r)),
	}
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
		case ro.Deferred:
			return fmt.Sprintf("repository %s deferred: locked by another execution", ro.Repository)
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

	l, ok, err := lock.AcquireRepository(opts.LockDir, repoName)
	if err != nil {
		outcome.BackupErr = fmt.Errorf("acquiring lock for repository %q: %w", repoName, err)
		return outcome
	}
	if !ok {
		outcome.Deferred = true
		return outcome
	}
	defer l.Unlock()

	repo := cfg.Repositories[repoName]
	job := cfg.Backups[jobName]

	if _, err := opts.Restic.Backup(ctx, repo, job.Source.Paths, tags); err != nil {
		outcome.BackupErr = err
		return outcome
	}

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
