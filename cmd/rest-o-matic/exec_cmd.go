package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"rest-o-matic/internal/config"
	"rest-o-matic/internal/execution"
)

// execMessagePrefix marks every message exec produces itself, so it stays
// distinguishable from restic's own output - which, unlike every other
// command here, is passed through to the user unmodified and therefore
// shares the same stream.
const execMessagePrefix = "rest-o-matic: "

var execForce bool

var execCmd = &cobra.Command{
	Use:   "exec <repository> -- <restic subcommand and args>",
	Short: "Run a restic subcommand directly against a configured repository",
	Long: `exec injects a configured repository's connection info and passes
everything after "--" straight to the real restic binary. Output and the
exit code are restic's own, unmodified.

Two safeguards apply. First, exec mirrors restic's own shared/exclusive
locking: commands that only need a shared lock run immediately, everything
else (including any subcommand exec doesn't recognize) waits for rest-o-matic's
own per-repository guard, same as backup/forget use - --force skips this
specific wait only. Second, "forget", "tag", and "restore latest" are
refused outright, with no override, when the target repository is used by
more than one job and no --tag was given - add --tag yourself, or run
restic directly if you really want to bypass this.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dashAt := cmd.ArgsLenAtDash()
		if dashAt != 1 {
			return fmt.Errorf("usage: rest-o-matic exec <repository> -- <restic subcommand and args>")
		}
		repoName := args[0]
		resticArgs := args[1:]

		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		if _, ok := cfg.Repositories[repoName]; !ok {
			fmt.Fprintf(os.Stderr, "%srepository %q is not defined in the config\n", execMessagePrefix, repoName)
			os.Exit(1)
		}

		opts := execution.Options{Restic: execution.NewResticRunner(), LockDir: lockDir()}
		result, err := execution.Exec(context.Background(), cfg, repoName, resticArgs, execForce, opts)
		if err != nil {
			return err
		}

		switch result.Blocked {
		case execution.BlockedByLock:
			fmt.Fprintf(os.Stderr, "%srepository %q is in use by another execution; try again shortly, or pass --force to run anyway\n", execMessagePrefix, repoName)
		case execution.BlockedByGate:
			fmt.Fprintf(os.Stderr, "%srefusing to run %q against %q without --tag: shared by jobs: %s\n%sthis check cannot be bypassed with an option - add --tag <job-name>, or run restic directly outside of exec\n",
				execMessagePrefix, resticArgs[0], repoName, strings.Join(result.SharingJobs, ", "), execMessagePrefix)
		}

		os.Exit(result.ExitCode)
		return nil // unreachable; os.Exit above always terminates first
	},
}

func init() {
	execCmd.Flags().BoolVar(&execForce, "force", false, "skip rest-o-matic's own per-repository lock wait (restic's own locking still applies; does not affect the tag-safety check)")
}
