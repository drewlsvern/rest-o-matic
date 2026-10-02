package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/lock"
	"github.com/drewlsvern/rest-o-matic/internal/statedir"
)

// Reserved exit codes for when exec refuses to invoke restic at all. Chosen
// to avoid restic's own documented exit codes: as of restic 0.16, every
// subcommand documents "0 on success, non-zero on any error", and `backup`
// additionally documents exit code 3 specifically (some source data could
// not be read). Whenever restic is actually invoked, its own exit code is
// propagated untouched instead of either of these - see PassThrough.
const (
	ExitLockBlocked     = 20
	ExitGateBlocked     = 21
	ExitStateDirBlocked = 22
	ExitReadModeBlocked = 23
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

// checkStateDir is statedir.Check; a variable so tests can fake another
// owner without running as root.
var checkStateDir = statedir.Check

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

// readModeTag is the tag a snapshot taken in a non-direct read mode
// carries (see RunJob).
func readModeTag(mode string) string { return "restomatic-read=" + mode }

// restoreValueFlags lists restic flags that take a separate value -
// restore's own and restic's global ones - so restoreSnapshotArg can skip
// the value instead of mistaking it for the snapshot. A flag missing from
// this list makes its value look like a second positional argument, which
// restoreSnapshotArg reports as ambiguous: the check fails closed.
var restoreValueFlags = map[string]bool{
	"-t": true, "--target": true,
	"-i": true, "--include": true, "--iinclude": true, "--include-file": true, "--iinclude-file": true,
	"-e": true, "--exclude": true, "--iexclude": true, "--exclude-file": true, "--iexclude-file": true,
	"-H": true, "--host": true, "--path": true, "--tag": true, "--overwrite": true,
	"-r": true, "--repo": true, "--repository-file": true,
	"-p": true, "--password-file": true, "--password-command": true, "--key-hint": true,
	"-o": true, "--option": true, "--cache-dir": true, "--cacert": true, "--tls-client-cert": true,
	"--compression": true, "--pack-size": true, "--limit-download": true, "--limit-upload": true,
	"--retry-lock": true, "--stuck-request-timeout": true, "--http-user-agent": true,
}

// restoreSnapshotArg returns the snapshot a restore invocation (args[0] ==
// "restore") uses, without any ":subfolder" suffix. ok is false unless
// there is exactly one positional argument.
func restoreSnapshotArg(args []string) (snapshot string, ok bool) {
	var positional []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && restoreValueFlags[a] {
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) != 1 {
		return "", false
	}
	snapshot, _, _ = strings.Cut(positional[0], ":")
	return snapshot, true
}

// tagFilters returns the value of every --tag flag in args, in order.
func tagFilters(args []string) []string {
	var filters []string
	for i := 0; i < len(args); i++ {
		if v, ok := strings.CutPrefix(args[i], "--tag="); ok {
			filters = append(filters, v)
		} else if args[i] == "--tag" && i+1 < len(args) {
			filters = append(filters, args[i+1])
			i++
		}
	}
	return filters
}

// restoreReadModeCheck returns why restoring in mode must be refused, or ""
// if the snapshot was taken in that mode. Restoring a snapshot in another
// mode than it was taken in gives restored files, and the existing parent
// directories restic restores metadata for, the wrong owners.
func restoreReadModeCheck(ctx context.Context, r *ResticRunner, repo config.Repository, mode string, args []string) (string, error) {
	want := readModeTag(mode)
	snapshot, ok := restoreSnapshotArg(args)
	if !ok {
		return `cannot tell which snapshot this restore uses; with --job, give the snapshot ID (or "latest") as the only argument after "restore"`, nil
	}

	if snapshot == "latest" {
		filters := tagFilters(args)
		for _, f := range filters {
			if !slices.Contains(strings.Split(f, ","), want) {
				return fmt.Sprintf("restore latest with --job for a read_as: %s job must only match snapshots taken that way; add %s to every --tag filter, e.g. --tag %s,%s", mode, want, f, want), nil
			}
		}
		if len(filters) == 0 {
			return fmt.Sprintf("restore latest with --job for a read_as: %s job must only match snapshots taken that way; add --tag %s", mode, want), nil
		}
		return "", nil
	}

	tags, err := r.SnapshotTags(ctx, repo, snapshot)
	if err != nil {
		return fmt.Sprintf("could not check how snapshot %s was taken: %v", snapshot, err), nil
	}
	if slices.Contains(tags, want) {
		return "", nil
	}
	for _, t := range tags {
		if other, ok := strings.CutPrefix(t, "restomatic-read="); ok {
			return fmt.Sprintf("snapshot %s was taken with read_as: %s, not %s; restore it through a job using read_as: %s", snapshot, other, mode, other), nil
		}
	}
	return fmt.Sprintf("snapshot %s was taken with read_as: %s (it has no %s tag); restore it without --job", snapshot, config.ReadDirect, want), nil
}

// SnapshotTags returns the tags of one snapshot, looked up directly (never
// in a wrapped read mode) with `restic snapshots --json <id>`, which only
// reads the repository.
func (r *ResticRunner) SnapshotTags(ctx context.Context, repo config.Repository, id string) ([]string, error) {
	env, err := r.repoEnv(repo)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.binary(), "-r", repo.URL, "snapshots", "--json", id)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var snaps []struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(out, &snaps); err != nil {
		return nil, fmt.Errorf("parsing restic snapshots output: %w", err)
	}
	if len(snaps) != 1 {
		return nil, fmt.Errorf("no single snapshot matches %q", id)
	}
	return snaps[0].Tags, nil
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
	// BlockedByStateDir means the state directory (or a file in it) is
	// owned by another user, so taking the lock would create files that
	// lock that user out; see internal/statedir.
	BlockedByStateDir BlockReason = "state-dir"
	// BlockedByReadMode means a restore through `exec --job` would use a
	// snapshot not taken in that job's read mode, giving files (and their
	// existing parent directories) the wrong owners.
	BlockedByReadMode BlockReason = "read-mode"
)

// ExecResult describes the outcome of an exec invocation.
type ExecResult struct {
	Repository string
	Blocked    BlockReason
	// SharingJobs is populated only when Blocked == BlockedByGate.
	SharingJobs []string
	// StateDirErr is populated only when Blocked == BlockedByStateDir.
	StateDirErr *statedir.OwnerError
	// ReadModeMsg explains the refusal when Blocked == BlockedByReadMode.
	ReadModeMsg string
	// ExitCode is restic's own exit code when restic was actually
	// invoked (Blocked == NotBlocked), or one of the reserved
	// ExitLockBlocked/ExitGateBlocked/ExitStateDirBlocked/
	// ExitReadModeBlocked constants otherwise.
	ExitCode int
}

// Exec runs a restic subcommand against a configured repository: resolving
// its connection info, applying the lock-type-based concurrency guard
// (skippable with force) and the tag-safety gate (never skippable), then
// passing the remaining args through to restic transparently. mode is the
// read mode restic runs in (see config.Job.ReadAs): config.ReadDirect, or
// the mode of the job named by `exec --job`.
func Exec(ctx context.Context, cfg *config.Config, repoName, mode string, args []string, force bool, opts Options) (ExecResult, error) {
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

	if subcommand == "restore" && mode != "" && mode != config.ReadDirect {
		msg, err := restoreReadModeCheck(ctx, opts.Restic, repo, mode, args)
		if err != nil {
			return ExecResult{}, err
		}
		if msg != "" {
			return ExecResult{Repository: repoName, Blocked: BlockedByReadMode, ReadModeMsg: msg, ExitCode: ExitReadModeBlocked}, nil
		}
	}

	if !force && requiresExclusiveLock(subcommand) {
		// Only this path creates files in the state directory, so only
		// this path needs the ownership check.
		if opts.StateDir != "" {
			if err := checkStateDir(opts.StateDir); err != nil {
				var oe *statedir.OwnerError
				if !errors.As(err, &oe) {
					return ExecResult{}, err
				}
				return ExecResult{Repository: repoName, Blocked: BlockedByStateDir, StateDirErr: oe, ExitCode: ExitStateDirBlocked}, nil
			}
		}
		l, acquired, err := lock.AcquireRepository(opts.LockDir, repoName)
		if err != nil {
			return ExecResult{}, fmt.Errorf("acquiring lock for repository %q: %w", repoName, err)
		}
		if !acquired {
			return ExecResult{Repository: repoName, Blocked: BlockedByLock, ExitCode: ExitLockBlocked}, nil
		}
		defer l.Unlock()
	}

	exitCode, err := opts.Restic.PassThrough(ctx, repo, mode, args)
	if err != nil {
		return ExecResult{}, fmt.Errorf("repository %q: %w", repoName, err)
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
//
// Unlike a wrapped backup, a wrapped passthrough stays in the caller's
// process group: it runs in the foreground of a terminal, where Ctrl-C
// already reaches every process in that group, restic included.
func (r *ResticRunner) PassThrough(ctx context.Context, repo config.Repository, mode string, args []string) (exitCode int, err error) {
	fullArgs := append([]string{"-r", repo.URL}, args...)

	env, err := r.repoEnv(repo)
	if err != nil {
		return -1, err
	}
	cmd, err := r.command(ctx, mode, fullArgs)
	if err != nil {
		return -1, err
	}
	cmd.Env = env
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
