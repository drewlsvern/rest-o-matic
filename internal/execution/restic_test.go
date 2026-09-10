package execution

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"rest-o-matic/internal/config"
)

func requireRestic(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic binary not available on PATH; skipping integration test")
	}
}

// initRepo creates and initializes a fresh local restic repository in a temp
// directory, returning the config.Repository pointing at it.
func initRepo(t *testing.T) config.Repository {
	t.Helper()
	dir := t.TempDir()
	repo := config.Repository{
		Backend:  "local",
		URL:      filepath.Join(dir, "repo"),
		Password: "test-password",
	}
	cmd := exec.Command("restic", "-r", repo.URL, "init")
	cmd.Env = repoEnv(repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restic init failed: %v: %s", err, out)
	}
	return repo
}

type resticSnapshot struct {
	ShortID string   `json:"short_id"`
	Tags    []string `json:"tags"`
}

func listSnapshots(t *testing.T, repo config.Repository, tagFilterArgs ...string) []resticSnapshot {
	t.Helper()
	args := append([]string{"-r", repo.URL, "snapshots", "--json"}, tagFilterArgs...)
	cmd := exec.Command("restic", args...)
	cmd.Env = repoEnv(repo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restic snapshots failed: %v: %s", err, out)
	}
	var snaps []resticSnapshot
	if err := json.Unmarshal(out, &snaps); err != nil {
		t.Fatalf("parsing snapshots JSON: %v (output: %s)", err, out)
	}
	return snaps
}

func TestBackup_AutomaticJobTag(t *testing.T) {
	requireRestic(t)
	repo := initRepo(t)
	r := NewResticRunner()

	srcDir := t.TempDir()

	if _, err := r.Backup(context.Background(), repo, []string{srcDir}, []string{"postgres"}); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	snaps := listSnapshots(t, repo)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if !containsTag(snaps[0].Tags, "postgres") {
		t.Fatalf("expected snapshot to carry tag 'postgres', got %v", snaps[0].Tags)
	}
}

func TestBackup_AdditiveUserTags(t *testing.T) {
	requireRestic(t)
	repo := initRepo(t)
	r := NewResticRunner()
	srcDir := t.TempDir()

	// Automatic job-name tag plus a user-supplied tag, as RunJob would build it.
	tags := []string{"postgres", "prod"}
	if _, err := r.Backup(context.Background(), repo, []string{srcDir}, tags); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	snaps := listSnapshots(t, repo)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if !containsTag(snaps[0].Tags, "postgres") || !containsTag(snaps[0].Tags, "prod") {
		t.Fatalf("expected snapshot to carry both tags, got %v", snaps[0].Tags)
	}
}

func TestForget_TagScopedToOwnJobOnly(t *testing.T) {
	requireRestic(t)
	repo := initRepo(t)
	r := NewResticRunner()
	srcDir := t.TempDir()

	// Two jobs sharing one repository, per the config/proposal example.
	if _, err := r.Backup(context.Background(), repo, []string{srcDir}, []string{"documents"}); err != nil {
		t.Fatalf("Backup(documents): %v", err)
	}
	if _, err := r.Backup(context.Background(), repo, []string{srcDir}, []string{"postgres"}); err != nil {
		t.Fatalf("Backup(postgres): %v", err)
	}

	// Enforce postgres's retention; documents' snapshot must be untouched.
	if err := r.Forget(context.Background(), repo, "postgres", config.Retention{"hourly": 24}); err != nil {
		t.Fatalf("Forget(postgres): %v", err)
	}

	docSnaps := listSnapshots(t, repo, "--tag", "documents")
	if len(docSnaps) != 1 {
		t.Fatalf("expected documents' snapshot to be untouched by postgres's forget, got %d snapshots", len(docSnaps))
	}
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
