package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drewlsvern/rest-o-matic/internal/color"
	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
)

// execMessagePrefix marks every message exec produces itself, so it stays
// distinguishable from restic's own output - which, unlike every other
// command here, is passed through to the user unmodified and therefore
// shares the same stream.
const execMessagePrefix = "rest-o-matic: "

// execErrorPrefix and execWarnPrefix are execMessagePrefix coloured by the
// message's severity; with colour off they equal execMessagePrefix exactly.
func execErrorPrefix() string { return color.Stderr.Error("rest-o-matic:") + " " }
func execWarnPrefix() string  { return color.Stderr.Warn("rest-o-matic:") + " " }

var (
	execForce bool
	execJob   string
)

var execCmd = &cobra.Command{
	Use:   "exec <repository> [--job <job>] -- <restic subcommand and args>",
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
restic directly if you really want to bypass this.

Before taking rest-o-matic's own lock, exec also refuses if the state
directory or a file in it belongs to another user, since the lock file it
would create could lock that user's scheduled runs out.

With --job, restic runs in that job's read mode (its read_as setting), so
files from a read_as: podman-unshare job are listed and restored with the
owners they had when backed up. For such a job, a restore is refused unless
its snapshot was taken the same way (it carries restomatic-read=<mode>);
for "latest", every --tag filter must include that tag. --job adds no tag
filter itself and does not satisfy the tag-safety check.

When exec refuses without running restic, it exits with a code of its own:
20 (repository in use), 21 (tag-safety check), 22 (state directory owned
by another user) or 23 (restore in a different read mode from its
snapshot). Otherwise the exit code is restic's.`,
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
		repo, ok := cfg.Repositories[repoName]
		if !ok {
			fmt.Fprintf(os.Stderr, "%srepository %q is not defined in the config\n", execErrorPrefix(), repoName)
			os.Exit(1)
		}
		// Only the target repository is checked: problems elsewhere in the
		// config must not block exec, which is the tool for repairing things.
		repoErrs, repoWarns := config.CheckRepository(repoName, repo)
		for _, w := range repoWarns {
			fmt.Fprintf(os.Stderr, "%sconfig warning: %s\n", execWarnPrefix(), w)
		}
		if len(repoErrs) > 0 {
			for _, e := range repoErrs {
				fmt.Fprintf(os.Stderr, "%sconfig error: %s\n", execErrorPrefix(), e)
			}
			os.Exit(1)
		}

		mode := config.ReadDirect
		if execJob != "" {
			job, ok := cfg.Backups[execJob]
			if !ok {
				fmt.Fprintf(os.Stderr, "%sjob %q is not defined in the config\n", execErrorPrefix(), execJob)
				os.Exit(1)
			}
			if !slices.Contains(cfg.JobsReferencing(repoName), execJob) {
				fmt.Fprintf(os.Stderr, "%sjob %q does not back up to repository %q\n", execErrorPrefix(), execJob, repoName)
				os.Exit(1)
			}
			mode = job.ReadMode()
		}

		opts := execution.Options{Restic: execution.NewResticRunner(), LockDir: lockDir(), StateDir: stateDir}
		result, err := execution.Exec(context.Background(), cfg, repoName, mode, resticArgs, execForce, opts)
		if err != nil {
			return err
		}

		switch result.Blocked {
		case execution.BlockedByLock:
			fmt.Fprintf(os.Stderr, "%srepository %q is in use by another execution; try again shortly, or pass --force to run anyway\n", execWarnPrefix(), repoName)
		case execution.BlockedByGate:
			fmt.Fprintf(os.Stderr, "%srefusing to run %q against %q without --tag: shared by jobs: %s\n%sthis check cannot be bypassed with an option - add --tag <job-name>, or run restic directly outside of exec\n",
				execErrorPrefix(), resticArgs[0], repoName, strings.Join(result.SharingJobs, ", "), execErrorPrefix())
		case execution.BlockedByReadMode:
			fmt.Fprintf(os.Stderr, "%s%s\n", execErrorPrefix(), result.ReadModeMsg)
		case execution.BlockedByStateDir:
			for _, line := range strings.Split(result.StateDirErr.Error(), "\n") {
				fmt.Fprintf(os.Stderr, "%s%s\n", execErrorPrefix(), line)
			}
		}

		os.Exit(result.ExitCode)
		return nil // unreachable; os.Exit above always terminates first
	},
}

func init() {
	execCmd.Flags().StringVar(&execJob, "job", "", "run restic the way this job reads its files (its read_as), e.g. to restore a podman-unshare job's files with their original owners; the job must back up to the repository")
	execCmd.Flags().BoolVar(&execForce, "force", false, "skip rest-o-matic's own per-repository lock wait (restic's own locking still applies; does not affect the tag-safety check)")
}
