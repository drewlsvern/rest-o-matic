package main

import (
	"context"
	"fmt"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// executeWithSlot acquires a global concurrency slot (a cross-process
// limiter shared by tick and run alike) before running the job, deferring
// it if none is available. report says whether result should be printed and
// counted: true when the job ran or failed to get a slot (both are
// outcomes), false only when work was interrupted before the job began, so
// there is nothing to report.
func executeWithSlot(work, cleanup context.Context, cfg *config.Config, store *state.Store, name string, opts execution.Options) (result execution.JobResult, report bool) {
	slot, ok, err := lock.AcquireSlot(lockDir(), cfg.MaxConcurrent)
	if err != nil {
		return execution.JobResult{Job: name, HookErr: fmt.Errorf("acquiring concurrency slot: %w", err)}, true
	}
	if !ok {
		return execution.JobResult{Job: name, HookErr: fmt.Errorf("no concurrency slot available; deferring")}, true
	}
	defer slot.Unlock()
	if work.Err() != nil {
		return execution.JobResult{}, false
	}

	return executeAndRecord(work, cleanup, cfg, store, name, opts), true
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
		case ro.Deferred:
			fmt.Printf("  repository %s: %s (locked by another execution)\n", ro.Repository, color.Stdout.Warn("deferred"))
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
