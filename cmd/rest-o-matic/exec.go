package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// startKind says what became of an attempt to run a job.
type startKind int

const (
	// jobRan: the job ran (or couldn't be set up) and has a result to
	// print and count.
	jobRan startKind = iota
	// jobNotStarted: work was interrupted before the job began, including
	// while it was waiting, so there is nothing to report.
	jobNotStarted
	// jobAlreadyRunning: another execution of the job is running or
	// waiting to start. Neither a success nor a failure.
	jobAlreadyRunning
	// jobNotDue: tick only. Another execution ran the job after this tick
	// worked out what was due.
	jobNotDue
)

// executeWithSlot obtains everything a job needs before any of it runs, in
// a fixed order: the job's own lock (without waiting), the lock of every
// repository it backs up to (waiting, in sorted order so two jobs can never
// each hold a repository the other needs), then a global concurrency slot
// (waiting). All are cross-process and shared by tick and run alike, and
// all are held until the job's state has been recorded.
//
// stillDue, if non-nil, is consulted once the job lock is held: the caller's
// view of what is due may be stale by then, and while the lock is held
// nobody else can run the job.
func executeWithSlot(work, cleanup context.Context, cfg *config.Config, store *state.Store, name string, opts execution.Options, stillDue func() bool) (execution.JobResult, startKind) {
	setupFailed := func(err error) (execution.JobResult, startKind) {
		if work.Err() != nil {
			return execution.JobResult{}, jobNotStarted
		}
		return execution.JobResult{Job: name, HookErr: err}, jobRan
	}

	jobLock, ok, err := lock.AcquireJob(opts.LockDir, name)
	if err != nil {
		return setupFailed(fmt.Errorf("acquiring job lock: %w", err))
	}
	if !ok {
		return execution.JobResult{Job: name}, jobAlreadyRunning
	}
	defer jobLock.Unlock()

	if stillDue != nil && !stillDue() {
		return execution.JobResult{Job: name}, jobNotDue
	}

	for _, repo := range repositoryNames(cfg.Backups[name]) {
		l, err := lock.WaitRepository(work, opts.LockDir, repo, func() {
			fmt.Printf("job %s: waiting for repository %s\n", name, repo)
		})
		if err != nil {
			return setupFailed(fmt.Errorf("acquiring lock for repository %q: %w", repo, err))
		}
		defer l.Unlock()
	}

	slot, err := lock.WaitSlot(work, opts.LockDir, cfg.MaxConcurrent, func() {
		fmt.Printf("job %s: waiting for a free slot\n", name)
	})
	if err != nil {
		return setupFailed(fmt.Errorf("acquiring concurrency slot: %w", err))
	}
	defer slot.Unlock()
	if work.Err() != nil {
		return execution.JobResult{}, jobNotStarted
	}

	return executeAndRecord(work, cleanup, cfg, store, name, opts), jobRan
}

// repositoryNames returns the repositories job backs up to, sorted and
// without duplicates: sorted so locks are always taken in one order, and
// deduplicated so a job listing a repository twice doesn't wait on itself.
func repositoryNames(job config.Job) []string {
	seen := make(map[string]bool, len(job.Repositories))
	var names []string
	for _, ref := range job.Repositories {
		if !seen[ref.Name] {
			seen[ref.Name] = true
			names = append(names, ref.Name)
		}
	}
	sort.Strings(names)
	return names
}

func executeAndRecord(work, cleanup context.Context, cfg *config.Config, store *state.Store, name string, opts execution.Options) execution.JobResult {
	result := execution.RunJob(work, cleanup, cfg, name, opts)

	outcome := "success"
	if !result.Success() {
		outcome = "failed"
	}
	_ = store.Update(name, state.JobState{LastRun: time.Now(), LastOutcome: outcome})

	return result
}

func printResult(r execution.JobResult) {
	failed := color.Stdout.Error("FAILED")
	switch {
	case r.Interrupted && r.InterruptErr != nil:
		fmt.Printf("job %s: %s (%v)\n", r.Job, failed, r.InterruptErr)
	case r.HookErr != nil:
		fmt.Printf("job %s: %s (%v)\n", r.Job, failed, r.HookErr)
	case r.Success():
		fmt.Printf("job %s: %s\n", r.Job, color.Stdout.Success("OK"))
	default:
		fmt.Printf("job %s: %s\n", r.Job, failed)
	}
	for _, ro := range r.Repos {
		switch {
		case ro.BackupErr != nil:
			fmt.Printf("  repository %s: %s: %v\n", ro.Repository, color.Stdout.Error("backup failed"), ro.BackupErr)
		case ro.ForgetErr != nil:
			fmt.Printf("  repository %s: backup ok, %s: %v\n", ro.Repository, color.Stdout.Error("forget failed"), ro.ForgetErr)
		default:
			fmt.Printf("  repository %s: %s\n", ro.Repository, color.Stdout.Success("ok"))
		}
	}
	for _, err := range r.AlwaysErrs {
		fmt.Printf("  %s: %v\n", color.Stdout.Error("always hook failed"), err)
	}
	outcomeHook := "failure"
	if r.Success() {
		outcomeHook = "success"
	}
	for _, err := range r.OutcomeHookErrs {
		fmt.Printf("  %s: %v\n", color.Stdout.Warn(outcomeHook+" hook failed"), err)
	}
}
