package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/state"
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

		store := state.NewStore(statePath())
		opts := execution.Options{Restic: execution.NewResticRunner(), LockDir: lockDir()}

		result := executeWithSlot(cfg, store, jobName, opts)
		printResult(result)
		if !result.Success() {
			return fmt.Errorf("job %q failed", jobName)
		}
		return nil
	},
}
