package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

func testConfig(t *testing.T, jobName string, job config.Job, repos map[string]config.Repository) *config.Config {
	t.Helper()
	return &config.Config{
		MaxConcurrent: 2,
		Policies: map[string]config.Policy{
			"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 24}},
		},
		Repositories: repos,
		Backups:      map[string]config.Job{jobName: job},
	}
}

func TestRunJob_HooksRunOnceRegardlessOfRepositoryCount(t *testing.T) {
	requireRestic(t)
	repoA := initRepo(t)
	repoB := initRepo(t)
	srcDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "hook-calls")

	job := config.Job{
		Name:   "documents",
		Source: config.Source{Paths: []string{srcDir}},
		Hooks: config.Hooks{
			Before: []string{"echo before >> " + marker},
			After:  []string{"echo after >> " + marker},
		},
		Policy:       "hot",
		Repositories: []config.RepositoryRef{{Name: "a"}, {Name: "b"}},
	}
	cfg := testConfig(t, "documents", job, map[string]config.Repository{"a": repoA, "b": repoB})

	result := RunJob(context.Background(), cfg, "documents", Options{
		Restic:  NewResticRunner(),
		LockDir: t.TempDir(),
	})
	if !result.Success() {
		t.Fatalf("expected job to succeed, got %+v", result)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading hook marker file: %v", err)
	}
	content := string(data)
	if got := countOccurrences(content, "before"); got != 1 {
		t.Fatalf("expected before-hook to run exactly once, ran %d times (content: %q)", got, content)
	}
	if got := countOccurrences(content, "after"); got != 1 {
		t.Fatalf("expected after-hook to run exactly once, ran %d times (content: %q)", got, content)
	}
}

func TestRunJob_BeforeHookFailureAbortsBackupButRunsAfterHooks(t *testing.T) {
	requireRestic(t)
	repo := initRepo(t)
	srcDir := t.TempDir()
	afterMarker := filepath.Join(t.TempDir(), "after-ran")

	job := config.Job{
		Name:   "postgres",
		Source: config.Source{Paths: []string{srcDir}},
		Hooks: config.Hooks{
			Before: []string{"exit 1"},
			After:  []string{"touch " + afterMarker},
		},
		Policy:       "hot",
		Repositories: []config.RepositoryRef{{Name: "nas"}},
	}
	cfg := testConfig(t, "postgres", job, map[string]config.Repository{"nas": repo})

	result := RunJob(context.Background(), cfg, "postgres", Options{
		Restic:  NewResticRunner(),
		LockDir: t.TempDir(),
	})

	if result.HookErr == nil {
		t.Fatal("expected HookErr to be set on before-hook failure")
	}
	if result.Success() {
		t.Fatal("expected job to be reported as failed")
	}
	if len(result.Repos) != 0 {
		t.Fatalf("expected no repository backup attempts, got %v", result.Repos)
	}
	if _, err := os.Stat(afterMarker); err != nil {
		t.Fatal("expected after-hook to still run for best-effort cleanup")
	}

	snaps := listSnapshots(t, repo)
	if len(snaps) != 0 {
		t.Fatalf("expected no snapshot to be created when the before-hook fails, got %d", len(snaps))
	}
}

func TestRunJob_IndependentPerRepositoryBackup(t *testing.T) {
	requireRestic(t)
	goodRepo := initRepo(t)
	// An uninitialized repository path: restic backup against it will fail.
	badRepo := config.Repository{Backend: "local", URL: filepath.Join(t.TempDir(), "never-initialized"), Password: "x"}
	srcDir := t.TempDir()

	job := config.Job{
		Name:         "documents",
		Source:       config.Source{Paths: []string{srcDir}},
		Policy:       "hot",
		Repositories: []config.RepositoryRef{{Name: "bad"}, {Name: "good"}},
	}
	cfg := testConfig(t, "documents", job, map[string]config.Repository{"bad": badRepo, "good": goodRepo})

	result := RunJob(context.Background(), cfg, "documents", Options{
		Restic:  NewResticRunner(),
		LockDir: t.TempDir(),
	})

	if result.Success() {
		t.Fatal("expected overall job result to reflect the bad repository's failure")
	}
	if len(result.Repos) != 2 {
		t.Fatalf("expected both repositories to be attempted, got %d", len(result.Repos))
	}

	var badOutcome, goodOutcome *RepoOutcome
	for i := range result.Repos {
		switch result.Repos[i].Repository {
		case "bad":
			badOutcome = &result.Repos[i]
		case "good":
			goodOutcome = &result.Repos[i]
		}
	}
	if badOutcome == nil || badOutcome.BackupErr == nil {
		t.Fatalf("expected the bad repository to have a backup error, got %+v", badOutcome)
	}
	if goodOutcome == nil || !goodOutcome.ok() {
		t.Fatalf("expected the good repository to succeed despite the other's failure, got %+v", goodOutcome)
	}

	snaps := listSnapshots(t, goodRepo)
	if len(snaps) != 1 {
		t.Fatalf("expected the good repository to have received its backup, got %d snapshots", len(snaps))
	}
}

func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			count++
			i += len(substr) - 1
		}
	}
	return count
}
