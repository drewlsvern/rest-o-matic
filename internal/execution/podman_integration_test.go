//go:build !windows

package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// requirePodmanUnshare skips unless podman can enter the user's rootless
// namespace here (it needs podman plus /etc/subuid entries for the user).
func requirePodmanUnshare(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("podman", "unshare", "true").CombinedOutput(); err != nil {
		t.Skipf("podman unshare not usable here; skipping: %v: %s", err, out)
	}
}

// subuidOwnedSecret creates dir/secret, mode 0600, owned by the first
// subordinate uid (uid 1 inside the namespace) - what a rootless container
// running as a non-root user leaves in a bind mount.
func subuidOwnedSecret(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("podman", "unshare", "chown", "1:1", path).CombinedOutput(); err != nil {
		t.Fatalf("podman unshare chown: %v: %s", err, out)
	}
	if uidOf(t, path) == os.Getuid() {
		t.Fatal("expected the secret to belong to a subordinate uid")
	}
	return path
}

func uidOf(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Sys().(*syscall.Stat_t).Uid)
}

func TestPodmanUnshare_BackupAndRestoreSubuidOwnedFile(t *testing.T) {
	requireRestic(t)
	requirePodmanUnshare(t)
	repo := initRepo(t)
	src := t.TempDir()
	secret := subuidOwnedSecret(t, src)
	r := NewResticRunner()

	// Direct: restic can't read the file, exits 3, and the hint points at
	// podman-unshare since podman is on PATH.
	_, err := r.Backup(context.Background(), repo, config.ReadDirect, []string{src}, []string{"gitea"})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "read_as: "+config.ReadPodmanUnshare) {
		t.Fatalf("expected exit 3 with the podman-unshare hint, got %v", err)
	}

	// podman-unshare: the same file is read. The password only reaches
	// restic if podman passes the environment through.
	id, err := r.Backup(context.Background(), repo, config.ReadPodmanUnshare, []string{src}, []string{"gitea"})
	if err != nil || id == "" {
		t.Fatalf("expected the wrapped backup to succeed with a snapshot id, got %q, %v", id, err)
	}

	// Restoring through the same mode gives the file back its host owner.
	target := t.TempDir()
	code, err := r.PassThrough(context.Background(), repo, config.ReadPodmanUnshare, []string{"restore", id, "--target", target})
	if err != nil || code != 0 {
		t.Fatalf("wrapped restore: code %d, %v", code, err)
	}
	restored := filepath.Join(target, secret)
	info, err := os.Stat(restored)
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if got, want := uidOf(t, restored), uidOf(t, secret); got != want {
		t.Errorf("restored owner uid = %d, want %d", got, want)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("restored mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestPodmanUnshare_PassesExitCodeThrough(t *testing.T) {
	requireRestic(t)
	requirePodmanUnshare(t)
	repo := initRepo(t)
	r := NewResticRunner()

	args := []string{"snapshots", "--no-such-flag"}
	direct, err := r.PassThrough(context.Background(), repo, config.ReadDirect, args)
	if err != nil || direct == 0 {
		t.Fatalf("expected restic to fail directly, got code %d, %v", direct, err)
	}
	wrapped, err := r.PassThrough(context.Background(), repo, config.ReadPodmanUnshare, args)
	if err != nil || wrapped != direct {
		t.Fatalf("expected podman unshare to pass exit code %d through, got %d, %v", direct, wrapped, err)
	}
}

func TestPodmanUnshare_CancelReachesRestic(t *testing.T) {
	requirePodmanUnshare(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "signal")
	started := filepath.Join(dir, "started")
	fake := filepath.Join(dir, "restic")
	script := `#!/bin/sh
trap 'echo INT > "$MARKER"; kill $child; exit 130' INT
trap 'echo TERM > "$MARKER"; kill $child; exit 143' TERM
echo $$ > "$STARTED"
sleep 30 >/dev/null 2>&1 &
child=$!
wait
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := config.Repository{Backend: "local", URL: filepath.Join(dir, "repo"), Password: "x",
		Env: map[string]string{"MARKER": marker, "STARTED": started}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := (&ResticRunner{Path: fake}).Backup(ctx, repo, config.ReadPodmanUnshare, []string{dir}, []string{"job"})
		done <- err
	}()

	var pid string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(started); err == nil && len(b) > 0 {
			pid = strings.TrimSpace(string(b))
			break
		}
	}
	if pid == "" {
		t.Fatal("fake restic never started under podman unshare")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(resticWaitDelay + 5*time.Second):
		t.Fatal("cancelled wrapped backup did not return")
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("restic under podman unshare received no signal (killed outright?): %v", err)
	}
	if strings.TrimSpace(string(got)) != "INT" {
		t.Fatalf("expected restic to receive SIGINT, got %q", got)
	}
	// The pid is as seen from the host, since podman unshare only enters a
	// user namespace, not a pid namespace.
	if err := exec.Command("kill", "-0", pid).Run(); err == nil {
		t.Fatalf("restic (pid %s) is still running after cancel", pid)
	}
}
