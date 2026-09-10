package main

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/schedule"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

var tickCmd = &cobra.Command{
	Use:   "tick",
	Short: "Evaluate every job's schedule, run whichever are due, then exit",
	Long: `tick is the only scheduling primitive rest-o-matic has: it does not run as a
daemon and does not write to the system crontab. Wire up one external
periodic trigger (cron, launchd, etc.) to invoke "rest-o-matic tick"
repeatedly; each invocation decides for itself which jobs are due and exits
once they've all finished.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadAndValidate()
		if err != nil {
			return err
		}

		store := state.NewStore(statePath())
		st, err := store.Load()
		if err != nil {
			return fmt.Errorf("loading state: %w", err)
		}

		now := time.Now()
		var due []string
		for name := range cfg.Backups {
			sched, err := cfg.EffectiveSchedule(name)
			if err != nil {
				fmt.Println("skipping job", name+":", err)
				continue
			}
			isDue, err := schedule.Due(sched, st.Jobs[name].LastRun, now)
			if err != nil {
				fmt.Println("skipping job", name+":", err)
				continue
			}
			if isDue {
				due = append(due, name)
			}
		}
		// Deterministic order within this tick's due set; a real
		// due-since timestamp tie-break isn't needed since Due only
		// reports a boolean, not how overdue a job is.
		sort.Strings(due)

		opts := execution.Options{Restic: execution.NewResticRunner(), LockDir: lockDir()}
		results := dispatch(cfg, store, due, opts)

		failed := 0
		for _, r := range results {
			printResult(r)
			if !r.Success() {
				failed++
			}
		}
		fmt.Printf("tick: %d job(s) due, %d succeeded, %d failed\n", len(due), len(due)-failed, failed)
		if failed > 0 {
			return fmt.Errorf("%d/%d due job(s) failed", failed, len(due))
		}
		return nil
	},
}

// dispatch runs the due set through an in-process worker pool bounded to
// cfg.MaxConcurrent, preserving the FIFO order of due (jobs are already
// sorted by the caller). Each job execution additionally takes a
// cross-process concurrency slot and per-repository locks (see exec.go and
// internal/lock), so the same cap holds even if another process (a
// concurrently-running tick, or a manual `run`) is also executing jobs.
func dispatch(cfg *config.Config, store *state.Store, due []string, opts execution.Options) []execution.JobResult {
	sem := make(chan struct{}, cfg.MaxConcurrent)
	var wg sync.WaitGroup
	results := make([]execution.JobResult, len(due))

	for i, name := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, name string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = executeWithSlot(cfg, store, name, opts)
		}(i, name)
	}
	wg.Wait()
	return results
}
