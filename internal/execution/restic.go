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
	"strconv"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/secrets"
)

// ResticRunner invokes the restic binary. Path defaults to "restic" (looked
// up on PATH); override it in tests. Secrets opens a repository's locked
// credentials; without it a repository that has any can't be used.
type ResticRunner struct {
	Path    string
	Secrets *secrets.Unlocker
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
// to the env vars restic expects. Locked values are unlocked here, in
// memory, for this one repository and only as restic is about to start.
func (r *ResticRunner) repoEnv(repo config.Repository) ([]string, error) {
	env := os.Environ()
	if !repo.Password.IsZero() {
		password, err := r.reveal("password", repo.Password)
		if err != nil {
			return nil, err
		}
		env = append(env, "RESTIC_PASSWORD="+password)
	}
	if repo.PasswordFile != "" {
		env = append(env, "RESTIC_PASSWORD_FILE="+repo.PasswordFile)
	}
	if repo.PasswordCommand != "" {
		env = append(env, "RESTIC_PASSWORD_COMMAND="+repo.PasswordCommand)
	}
	for k, v := range repo.Env {
		value, err := r.reveal("env "+k, v)
		if err != nil {
			return nil, err
		}
		env = append(env, k+"="+value)
	}
	return env, nil
}

// reveal returns a credential's plain text, unlocking it if need be. The
// error says which field couldn't be unlocked and why, and never includes
// any of the locked text.
func (r *ResticRunner) reveal(field string, s config.Secret) (string, error) {
	if !s.Locked {
		return s.Value, nil
	}
	if r.Secrets == nil {
		return "", fmt.Errorf("%s is locked, and no host key is configured", field)
	}
	value, err := r.Secrets.Reveal(s.Value)
	switch {
	case errors.Is(err, secrets.ErrNoHostKey):
		return "", fmt.Errorf("%s is locked, and this host has no key to unlock it: %w", field, err)
	case errors.Is(err, secrets.ErrNotForThisKey):
		return "", fmt.Errorf("%s is locked, but was %w", field, err)
	case err != nil:
		return "", fmt.Errorf("%s is locked and could not be unlocked: %w", field, err)
	}
	return value, nil
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
func (r *ResticRunner) Backup(ctx context.Context, repo config.Repository, paths []string, tags []string) (snapshotID string, err error) {
	args := []string{"-r", repo.URL, "backup"}
	args = append(args, paths...)
	for _, t := range tags {
		args = append(args, "--tag", t)
	}
	args = append(args, "--json")

	env, err := r.repoEnv(repo)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, r.binary(), args...)
	cmd.Env = env
	configureResticCancel(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		return "", fmt.Errorf("restic backup failed: %w: %s", runErr, stderr.String())
	}
	return parseSnapshotID(stdout.Bytes()), nil
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

	env, err := r.repoEnv(repo)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, r.binary(), args...)
	cmd.Env = env
	configureResticCancel(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restic forget failed: %w: %s", err, stderr.String())
	}
	return nil
}
