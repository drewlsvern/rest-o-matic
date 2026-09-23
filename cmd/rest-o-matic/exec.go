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
// it if none is available.
func executeWithSlot(cfg *config.Config, store *state.Store, name string, opts execution.Options) execution.JobResult {
	slot, ok, err := lock.AcquireSlot(lockDir(), cfg.MaxConcurrent)
	if err != nil {
		return execution.JobResult{Job: name, HookErr: fmt.Errorf("acquiring concurrency slot: %w", err)}
	}
	if !ok {
		return execution.JobResult{Job: name, HookErr: fmt.Errorf("no concurrency slot available; deferring")}
	}
	defer slot.Unlock()

	return executeAndRecord(cfg, store, name, opts)
}

func executeAndRecord(cfg *config.Config, store *state.Store, name string, opts execution.Options) execution.JobResult {
	result := execution.RunJob(context.Background(), cfg, name, opts)

	outcome := "success"
	if !result.Success() {
		outcome = "failed"
	}
	_ = store.Update(name, state.JobState{LastRun: time.Now(), LastOutcome: outcome})

	return result
}

func printResult(r execution.JobResult) {
	if r.HookErr != nil {
		fmt.Printf("job %s: %s (%v)\n", r.Job, color.Stdout.Error("FAILED"), r.HookErr)
		return
	}
	status := color.Stdout.Success("OK")
	if !r.Success() {
		status = color.Stdout.Error("FAILED")
	}
	fmt.Printf("job %s: %s\n", r.Job, status)
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
}
