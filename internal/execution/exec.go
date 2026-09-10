package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"rest-o-matic/internal/config"
	"rest-o-matic/internal/lock"
)

// Reserved exit codes for when exec refuses to invoke restic at all. Chosen
// to avoid restic's own documented exit codes: as of restic 0.16, every
// subcommand documents "0 on success, non-zero on any error", and `backup`
// additionally documents exit code 3 specifically (some source data could
// not be read). Whenever restic is actually invoked, its own exit code is
// propagated untouched instead of either of these - see PassThrough.
const (
	ExitLockBlocked = 20
	ExitGateBlocked = 21
)

// sharedLockSubcommands lists restic subcommands understood to take only a
// shared (non-exclusive) lock, based on restic's documented general design
// (read/inspection commands use a shared lock; anything that mutates the
// repository or its snapshot set uses an exclusive one). This isn't
// verified line-by-line against restic's source for every entry, so it's
// deliberately treated as provisional: requiresExclusiveLock defaults to
// true for anything not on this list, including future restic subcommands,
// so an inaccurate or stale list only ever costs an unnecessary wait, never
// unsafe concurrency.
var sharedLockSubcommands = map[string]struct{}{
	"snapshots": {},
	"ls":        {},
	"find":      {},
	"diff":      {},
	"stats":     {},
	"cat":       {},
	"check":     {},
	"mount":     {},
	"restore":   {},
}

func requiresExclusiveLock(subcommand string) bool {
	_, shared := sharedLockSubcommands[subcommand]
	return !shared
}

// hasTagFlag reports whether args contains a --tag flag, in either
// "--tag value" or "--tag=value" form. This is deliberately a light
// heuristic rather than a full restic flag parser - see design.md.
func hasTagFlag(args []string) bool {
	for _, a := range args {
		if a == "--tag" || strings.HasPrefix(a, "--tag=") {
			return true
		}
	}
	return false
}

// isRelativeRestoreSelector reports whether a restore invocation's snapshot
// argument is a relative selector ("latest") rather than an explicit
// snapshot ID. Restic accepts "latest" as a literal snapshot ID value
// (optionally narrowed by separate --host/--tag/--path flags), so checking
// for that literal token anywhere in args - rather than trying to track
// which positional argument is the snapshot ID - is simpler and more
// robust to argument ordering than a full flag parse.
func isRelativeRestoreSelector(args []string) bool {
	for _, a := range args {
		if a == "latest" {
			return true
		}
	}
	return false
}

// GateBlocked reports whether the tag-safety gate refuses this invocation:
// forget/tag are refused without --tag when the repository is shared by
// more than one job; restore is refused the same way only when using the
// relative "latest" selector; prune is exempt; everything else is
// ungated. No option overrides this - see design.md and the spec.
func gateBlocked(subcommand string, args []string, sharingJobs []string) bool {
	if len(sharingJobs) < 2 {
		return false
	}
	switch subcommand {
	case "forget", "tag":
		return !hasTagFlag(args)
	case "restore":
		return isRelativeRestoreSelector(args) && !hasTagFlag(args)
	default:
		return false
	}
}

// BlockReason identifies why exec refused to invoke restic.
type BlockReason string

const (
	// NotBlocked means restic was actually invoked.
	NotBlocked BlockReason = ""
	// BlockedByLock means another execution currently holds the
	// per-repository guard; retrying later (or --force) may succeed.
	BlockedByLock BlockReason = "lock"
	// BlockedByGate means the tag-safety gate refused the command; no
	// option bypasses this, the command itself must change.
	BlockedByGate BlockReason = "gate"
)

// ExecResult describes the outcome of an exec invocation.
type ExecResult struct {
	Repository string
	Blocked    BlockReason
	// SharingJobs is populated only when Blocked == BlockedByGate.
	SharingJobs []string
	// ExitCode is restic's own exit code when restic was actually
	// invoked (Blocked == NotBlocked), or one of the reserved
	// ExitLockBlocked/ExitGateBlocked constants otherwise.
	ExitCode int
}

// Exec runs a restic subcommand against a configured repository: resolving
// its connection info, applying the lock-type-based concurrency guard
// (skippable with force) and the tag-safety gate (never skippable), then
// passing the remaining args through to restic transparently.
func Exec(ctx context.Context, cfg *config.Config, repoName string, args []string, force bool, opts Options) (ExecResult, error) {
	repo, ok := cfg.Repositories[repoName]
	if !ok {
		return ExecResult{}, fmt.Errorf("no such repository %q", repoName)
	}

	var subcommand string
	if len(args) > 0 {
		subcommand = args[0]
	}

	sharingJobs := cfg.JobsReferencing(repoName)
	if gateBlocked(subcommand, args, sharingJobs) {
		return ExecResult{
			Repository:  repoName,
			Blocked:     BlockedByGate,
			SharingJobs: sharingJobs,
			ExitCode:    ExitGateBlocked,
		}, nil
	}

	if !force && requiresExclusiveLock(subcommand) {
		l, acquired, err := lock.AcquireRepository(opts.LockDir, repoName)
		if err != nil {
			return ExecResult{}, fmt.Errorf("acquiring lock for repository %q: %w", repoName, err)
		}
		if !acquired {
			return ExecResult{Repository: repoName, Blocked: BlockedByLock, ExitCode: ExitLockBlocked}, nil
		}
		defer l.Unlock()
	}

	exitCode, err := opts.Restic.PassThrough(ctx, repo, args)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{Repository: repoName, ExitCode: exitCode}, nil
}

// PassThrough invokes restic with args against repo, connecting stdin,
// stdout, and stderr directly to the calling process rather than capturing
// them - unlike Backup/Forget, exec's output must reach the user exactly as
// restic produced it (some subcommands write binary data to stdout, others
// are long-running/interactive). It returns restic's own exit code when
// restic actually runs, even on failure; the returned error is non-nil only
// when restic could not be started at all.
func (r *ResticRunner) PassThrough(ctx context.Context, repo config.Repository, args []string) (exitCode int, err error) {
	fullArgs := append([]string{"-r", repo.URL}, args...)

	cmd := exec.CommandContext(ctx, r.binary(), fullArgs...)
	cmd.Env = repoEnv(repo)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	if runErr == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, fmt.Errorf("running restic: %w", runErr)
}
