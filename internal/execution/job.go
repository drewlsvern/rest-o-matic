package execution

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"rest-o-matic/internal/config"
	"rest-o-matic/internal/lock"
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
}

// Success reports whether the job's hooks and every repository it targeted
// all succeeded.
func (r JobResult) Success() bool {
	if r.HookErr != nil {
		return false
	}
	for _, ro := range r.Repos {
		if !ro.ok() {
			return false
		}
	}
	return true
}

func runHooks(ctx context.Context, commands []string) error {
	for _, c := range commands {
		cmd := exec.CommandContext(ctx, "sh", "-c", c)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("hook %q failed: %w: %s", c, err, out.String())
		}
	}
	return nil
}

// RunJob executes a single job end-to-end:
//  1. before hooks (once; a failure aborts all repository backups, but
//     after hooks still run for best-effort cleanup)
//  2. an independent backup+forget attempt against each configured
//     repository, each guarded by that repository's cross-process lock
//  3. after hooks (once, regardless of backup outcome)
func RunJob(ctx context.Context, cfg *config.Config, jobName string, opts Options) JobResult {
	job := cfg.Backups[jobName]
	result := JobResult{Job: jobName}

	if err := runHooks(ctx, job.Hooks.Before); err != nil {
		result.HookErr = err
		_ = runHooks(ctx, job.Hooks.After) // best-effort cleanup even though the job failed
		return result
	}

	tags := append([]string{jobName}, job.Tags...)

	for _, ref := range job.Repositories {
		result.Repos = append(result.Repos, runRepo(ctx, cfg, jobName, ref.Name, tags, opts))
	}

	_ = runHooks(ctx, job.Hooks.After)
	return result
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
