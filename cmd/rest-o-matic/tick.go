package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/schedule"
	"github.com/drewlsvern/rest-o-matic/internal/state"
	"github.com/drewlsvern/rest-o-matic/internal/statedir"
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
		if err := statedir.Check(stateDir); err != nil {
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
			isDue, err := jobDue(cfg, st, name, now)
			if err != nil {
				fmt.Println(color.Stdout.Warn("skipping job"), name+":", err)
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
		work, cleanup, stop := interruptContexts()
		defer stop()
		attempts := dispatch(work, cleanup, cfg, store, due, opts)

		// A job another execution ran in the meantime was not due after
		// all, so it leaves the count.
		total, succeeded, failed, running := len(due), 0, 0, 0
		for _, a := range attempts {
			switch a.kind {
			case jobRan:
				printResult(a.result)
				if a.result.Success() {
					succeeded++
				} else {
					failed++
				}
			case jobAlreadyRunning:
				fmt.Printf("job %s: %s, skipped\n", a.result.Job, color.Stdout.Warn("already running"))
				running++
			case jobNotDue:
				total--
			}
		}
		notStarted := total - succeeded - failed - running

		succeededMsg := fmt.Sprintf("%d succeeded", succeeded)
		if succeeded > 0 {
			succeededMsg = color.Stdout.Success(succeededMsg)
		}
		failedMsg := fmt.Sprintf("%d failed", failed)
		if failed > 0 {
			failedMsg = color.Stdout.Error(failedMsg)
		}
		summary := fmt.Sprintf("tick: %d job(s) due, %s, %s", total, succeededMsg, failedMsg)
		if running > 0 {
			summary += fmt.Sprintf(", %d already running", running)
		}
		if notStarted > 0 {
			summary += ", " + color.Stdout.Warn(fmt.Sprintf("%d not started", notStarted)) + fmt.Sprintf(" (%v)", context.Cause(work))
		}
		fmt.Println(summary)
		if failed > 0 || notStarted > 0 {
			return fmt.Errorf("%d/%d due job(s) failed or not started", failed+notStarted, total)
		}
		return nil
	},
}

// jobDue reports whether the named job's schedule says it should run at
// now, given its last recorded run in st.
func jobDue(cfg *config.Config, st *state.State, name string, now time.Time) (bool, error) {
	sched, err := cfg.EffectiveSchedule(name)
	if err != nil {
		return false, err
	}
	return schedule.Due(sched, st.Jobs[name].LastRun, now)
}

// attempt is what became of one due job in a tick. result is meaningful
// only for jobRan; for the other kinds only result.Job may be set.
type attempt struct {
	result execution.JobResult
	kind   startKind
}

// dispatch runs the due set through an in-process worker pool bounded to
// cfg.MaxConcurrent, preserving the FIFO order of due (jobs are already
// sorted by the caller). Each job execution additionally takes its own
// job lock, its repositories' locks and a concurrency slot, all
// cross-process (see exec.go and internal/lock), so the same rules hold
// even if another process (a concurrently-running tick, or a manual `run`)
// is also executing jobs.
//
// Once work is cancelled no further job starts. The returned attempts are
// in due order, one per job.
func dispatch(work, cleanup context.Context, cfg *config.Config, store *state.Store, due []string, opts execution.Options) []attempt {
	sem := make(chan struct{}, cfg.MaxConcurrent)
	var wg sync.WaitGroup
	attempts := make([]attempt, len(due))
	for i := range attempts {
		attempts[i].kind = jobNotStarted
	}

	for i, name := range due {
		select {
		case sem <- struct{}{}:
		case <-work.Done():
		}
		if work.Err() != nil {
			break
		}
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			defer func() { <-sem }()
			// The due list was worked out before any job started, and
			// this job may have waited behind sem since. If another
			// execution has run it meanwhile, it must not run again. A
			// state that can't be read leaves the earlier answer standing.
			stillDue := func() bool {
				st, err := store.Load()
				if err != nil {
					return true
				}
				isDue, err := jobDue(cfg, st, name, time.Now())
				return err != nil || isDue
			}
			attempts[i].result, attempts[i].kind = executeWithSlot(work, cleanup, cfg, store, name, opts, stillDue)
		}(i, name)
	}
	wg.Wait()
	return attempts
}
