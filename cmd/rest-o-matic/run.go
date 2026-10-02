package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/state"
	"github.com/drewlsvern/rest-o-matic/internal/statedir"
)

var runCmd = &cobra.Command{
	Use:   "run <job>",
	Short: "Run a specific job immediately, regardless of its schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobName := args[0]

		cfg, err := loadAndValidate()
		if err != nil {
			return err
		}
		if _, ok := cfg.Backups[jobName]; !ok {
			return fmt.Errorf("no such job %q", jobName)
		}
		if err := statedir.Check(stateDir); err != nil {
			return err
		}

		store := state.NewStore(statePath())
		opts := execution.Options{Restic: execution.NewResticRunner(), LockDir: lockDir()}

		work, cleanup, stop := interruptContexts()
		defer stop()

		result, kind := executeWithSlot(work, cleanup, cfg, store, jobName, opts, nil)
		switch kind {
		case jobAlreadyRunning:
			return fmt.Errorf("job %q is already running", jobName)
		case jobNotStarted:
			return fmt.Errorf("job %q not started: %v", jobName, context.Cause(work))
		}
		printResult(result)
		if !result.Success() {
			return fmt.Errorf("job %q failed", jobName)
		}
		return nil
	},
}
