package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"rest-o-matic/internal/config"
)

var (
	configPath string
	stateDir   string
)

var rootCmd = &cobra.Command{
	Use:           "rest-o-matic",
	Short:         "A simple Restic wrapper",
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "rest-o-matic.yaml", "path to the config file")
	rootCmd.PersistentFlags().StringVar(&stateDir, "state-dir", ".rest-o-matic", "directory for state and lock files")
	rootCmd.AddCommand(validateCmd, runCmd, tickCmd, execCmd)
}

func statePath() string { return filepath.Join(stateDir, "state.json") }
func lockDir() string   { return filepath.Join(stateDir, "locks") }

// loadAndValidate loads the config and rejects it if validation finds any
// problems, printing every problem found rather than just the first.
func loadAndValidate() (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if errs := config.Validate(cfg); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "config error:", e)
		}
		return nil, fmt.Errorf("%d config validation error(s)", len(errs))
	}
	return cfg, nil
}
