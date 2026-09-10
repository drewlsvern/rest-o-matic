package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/config"
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
	appVersion := readBuildInfo()
	// cobra's no-args/help output prints .Long (falling back to .Short) as
	// a block above the Usage/Available Commands listing, and never
	// references .Version at all (see design.md) - so the short version
	// line is added as a second line of Long rather than by fighting
	// cobra's usage template.
	rootCmd.Long = "A simple Restic wrapper\n" + appVersion.shortLine()
	// rootCmd.Version enables cobra's own --version/-v flag; the template
	// override below makes it print the detailed block as-is instead of
	// cobra's default one-line "<name> version <value>" wrapping.
	rootCmd.Version = appVersion.detailedBlock()
	rootCmd.SetVersionTemplate("{{.Version}}\n")

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
