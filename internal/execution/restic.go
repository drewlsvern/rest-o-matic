// Package execution runs a backup job end-to-end: hooks, restic backup per
// repository, automatic tagging, and restic forget for retention.
package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// ResticRunner invokes the restic binary. Path defaults to "restic" (looked
// up on PATH); override it in tests. Podman is the podman binary used by
// the podman-unshare read mode, "podman" on PATH when empty.
type ResticRunner struct {
	Path   string
	Podman string
}

// NewResticRunner returns a runner that invokes "restic" from PATH.
func NewResticRunner() *ResticRunner {
	return &ResticRunner{Path: "restic"}
}

func (r *ResticRunner) binary() string {
	if r.Path != "" {
		return r.Path
	}
	return "restic"
}

// command builds the restic invocation for args in the given read mode
// (see config.Job.ReadAs). In podman-unshare mode restic runs as
// `podman unshare <restic> <args...>`, with restic resolved to an absolute
// path first so it doesn't depend on PATH inside the namespace. The
// environment is set by the caller and passes through podman unchanged.
func (r *ResticRunner) command(ctx context.Context, mode string, args []string) (*exec.Cmd, error) {
	switch mode {
	case "", config.ReadDirect:
		return exec.CommandContext(ctx, r.binary(), args...), nil
	case config.ReadPodmanUnshare:
		restic, err := exec.LookPath(r.binary())
		if err != nil {
			return nil, fmt.Errorf("finding restic: %w", err)
		}
		if restic, err = filepath.Abs(restic); err != nil {
			return nil, fmt.Errorf("finding restic: %w", err)
		}
		podman := r.Podman
		if podman == "" {
			podman = "podman"
		}
		if podman, err = exec.LookPath(podman); err != nil {
			return nil, fmt.Errorf("read_as: %s needs podman, which was not found: %w", mode, err)
		}
		return exec.CommandContext(ctx, podman, append([]string{"unshare", restic}, args...)...), nil
	}
	return nil, fmt.Errorf("unknown read mode %q", mode)
}

// resticWaitDelay is how long a cancelled restic gets to exit after SIGINT
// before it is killed outright.
const resticWaitDelay = 10 * time.Second

// configureResticCancel makes cancelling ctx send restic SIGINT instead of
// exec's default SIGKILL: on SIGINT restic removes its lock from the
// repository, whereas a killed restic leaves a stale lock behind. Where
// SIGINT can't be sent (Windows), it falls back to killing the process.
func configureResticCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = resticWaitDelay
}

// repoEnv builds the environment restic needs to reach a repository:
// the caller's own environment plus password/credential fields translated
// to the env vars restic expects.
func repoEnv(repo config.Repository) []string {
	env := os.Environ()
	if repo.Password != "" {
		env = append(env, "RESTIC_PASSWORD="+repo.Password)
	}
	if repo.PasswordFile != "" {
		env = append(env, "RESTIC_PASSWORD_FILE="+repo.PasswordFile)
	}
	if repo.PasswordCommand != "" {
		env = append(env, "RESTIC_PASSWORD_COMMAND="+repo.PasswordCommand)
	}
	for k, v := range repo.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// resticMessage is the subset of restic's --json output lines this package
// cares about. restic emits one JSON object per line; the line with
// message_type "summary" carries the resulting snapshot id.
type resticMessage struct {
	MessageType string `json:"message_type"`
	SnapshotID  string `json:"snapshot_id"`
}

func parseSnapshotID(output []byte) string {
	for _, line := range bytes.Split(output, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var msg resticMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.MessageType == "summary" {
			return msg.SnapshotID
		}
	}
	return ""
}

// Backup runs `restic backup` against repo for the given paths, tagging the
// resulting snapshot with every tag supplied (the caller is responsible for
// including the automatic job-name tag alongside any user tags). restic's
// own stderr output is preserved on failure per the design's "surface
// restic's errors, don't reinterpret them" decision.
func (r *ResticRunner) Backup(ctx context.Context, repo config.Repository, mode string, paths []string, tags []string) (snapshotID string, err error) {
	args := []string{"-r", repo.URL, "backup"}
	args = append(args, paths...)
	for _, t := range tags {
		args = append(args, "--tag", t)
	}
	args = append(args, "--json")

	cmd, err := r.command(ctx, mode, args)
	if err != nil {
		return "", err
	}
	cmd.Env = repoEnv(repo)
	if mode == "" || mode == config.ReadDirect {
		configureResticCancel(cmd)
	} else {
		configureWrappedResticCancel(cmd)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		err := fmt.Errorf("restic backup failed: %w: %s", runErr, stderr.String())
		if hint := unreadableHint(runErr, stderr.String(), mode); hint != "" {
			err = fmt.Errorf("%w\nhint: %s", err, hint)
		}
		return "", err
	}
	return parseSnapshotID(stdout.Bytes()), nil
}

// hostGOOS and lookPath are runtime.GOOS and exec.LookPath; variables so
// tests can pick which hint unreadableHint gives.
var (
	hostGOOS = runtime.GOOS
	lookPath = exec.LookPath
)

// unreadableHint explains a backup that failed because restic couldn't
// read some source files (exit code 3 with permission errors), or returns
// "" for any other failure. Files written by rootless Podman containers
// are the common cause, so a direct job on a Linux host with podman is
// pointed at read_as: podman-unshare.
func unreadableHint(runErr error, stderr, mode string) string {
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 3 || !strings.Contains(strings.ToLower(stderr), "permission denied") {
		return ""
	}
	if (mode == "" || mode == config.ReadDirect) && hostGOOS == "linux" {
		if _, err := lookPath("podman"); err == nil {
			return "some source files could not be read; if rootless Podman containers wrote them, set read_as: " + config.ReadPodmanUnshare + " on this job"
		}
	}
	return "some source files could not be read; run rest-o-matic as the user that owns them, or as root"
}

// retentionFlag maps a Retention key to the restic --keep-* flag it
// corresponds to. Unknown keys are ignored rather than rejected, since the
// config layer does not restrict what keys a policy may declare.
func retentionFlag(period string) (string, bool) {
	switch period {
	case "hourly":
		return "--keep-hourly", true
	case "daily":
		return "--keep-daily", true
	case "weekly":
		return "--keep-weekly", true
	case "monthly":
		return "--keep-monthly", true
	case "yearly":
		return "--keep-yearly", true
	default:
		return "", false
	}
}

// Forget runs `restic forget`, scoped to snapshots carrying jobTag, using
// the resolved effective retention for this (job, repository) pair. Scoping
// by tag is what keeps a shared repository's other jobs' snapshots safe.
func (r *ResticRunner) Forget(ctx context.Context, repo config.Repository, jobTag string, retention config.Retention) error {
	args := []string{"-r", repo.URL, "forget", "--tag", jobTag}
	for period, count := range retention {
		flag, ok := retentionFlag(period)
		if !ok {
			continue
		}
		args = append(args, flag, strconv.Itoa(count))
	}
	args = append(args, "--prune")

	cmd := exec.CommandContext(ctx, r.binary(), args...)
	cmd.Env = repoEnv(repo)
	configureResticCancel(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restic forget failed: %w: %s", err, stderr.String())
	}
	return nil
}
