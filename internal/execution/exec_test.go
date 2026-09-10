package execution

import (
	"context"
	"testing"

	"rest-o-matic/internal/config"
	"rest-o-matic/internal/lock"
)

// --- Lock-type classification (spec: Lock-Type-Based Concurrency Guard) ---

func TestRequiresExclusiveLock_KnownSharedLockCommandsDoNot(t *testing.T) {
	for cmd := range sharedLockSubcommands {
		if requiresExclusiveLock(cmd) {
			t.Errorf("expected %q to not require the exclusive lock", cmd)
		}
	}
}

func TestRequiresExclusiveLock_UnrecognizedCommandDefaultsToExclusive(t *testing.T) {
	for _, cmd := range []string{"backup", "forget", "prune", "tag", "copy", "rewrite", "init", "some-future-subcommand"} {
		if !requiresExclusiveLock(cmd) {
			t.Errorf("expected %q to require the exclusive lock (fail-closed default)", cmd)
		}
	}
}

// --- Tag-safety gate (spec: Tag-Safety Gate for Forget and Tag / Restore / Prune Exempt) ---

func TestGateBlocked_UntaggedForgetAgainstSharedRepoIsBlocked(t *testing.T) {
	if !gateBlocked("forget", []string{"forget", "--prune"}, []string{"documents", "postgres"}) {
		t.Fatal("expected untagged forget against a shared repository to be blocked")
	}
}

func TestGateBlocked_TaggedForgetAgainstSharedRepoIsAllowed(t *testing.T) {
	if gateBlocked("forget", []string{"forget", "--tag", "postgres", "--prune"}, []string{"documents", "postgres"}) {
		t.Fatal("expected tagged forget against a shared repository to be allowed")
	}
}

func TestGateBlocked_ForgetAgainstSingleJobRepoIsNeverBlocked(t *testing.T) {
	if gateBlocked("forget", []string{"forget", "--prune"}, []string{"documents"}) {
		t.Fatal("expected forget against a single-job repository to never be gated")
	}
}

func TestGateBlocked_UntaggedTagCommandAgainstSharedRepoIsBlocked(t *testing.T) {
	if !gateBlocked("tag", []string{"tag", "--set", "prod"}, []string{"documents", "postgres"}) {
		t.Fatal("expected untagged 'tag' subcommand against a shared repository to be blocked")
	}
}

func TestGateBlocked_RestoreLatestWithoutTagAgainstSharedRepoIsBlocked(t *testing.T) {
	if !gateBlocked("restore", []string{"restore", "latest", "--target", "/tmp/x"}, []string{"documents", "postgres"}) {
		t.Fatal("expected restore of 'latest' without --tag against a shared repository to be blocked")
	}
}

func TestGateBlocked_RestoreExplicitSnapshotIDIsNeverBlocked(t *testing.T) {
	if gateBlocked("restore", []string{"restore", "abc123", "--target", "/tmp/x"}, []string{"documents", "postgres"}) {
		t.Fatal("expected restore of an explicit snapshot ID to never be gated, regardless of --tag")
	}
}

func TestGateBlocked_RestoreLatestWithTagIsAllowed(t *testing.T) {
	if gateBlocked("restore", []string{"restore", "latest", "--tag", "postgres", "--target", "/tmp/x"}, []string{"documents", "postgres"}) {
		t.Fatal("expected restore of 'latest' with --tag to be allowed")
	}
}

func TestGateBlocked_PruneIsExemptEvenOnSharedRepo(t *testing.T) {
	if gateBlocked("prune", []string{"prune"}, []string{"documents", "postgres"}) {
		t.Fatal("expected prune to be exempt from the tag-safety gate")
	}
}

func TestGateBlocked_UngatedSubcommandsAreNeverBlocked(t *testing.T) {
	for _, cmd := range []string{"snapshots", "check", "ls", "backup"} {
		if gateBlocked(cmd, []string{cmd}, []string{"documents", "postgres"}) {
			t.Errorf("expected %q to never be gated by the tag-safety check", cmd)
		}
	}
}

// --- Exec orchestration: force scoping, lock deferral, gate precedence ---

func execTestConfig() *config.Config {
	return &config.Config{
		Repositories: map[string]config.Repository{
			"nas": {Backend: "local", URL: "/tmp/does-not-matter"},
		},
		Policies: map[string]config.Policy{
			"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 1}},
		},
		Backups: map[string]config.Job{
			"documents": {
				Name:         "documents",
				Source:       config.Source{Paths: []string{"/a"}},
				Policy:       "hot",
				Repositories: []config.RepositoryRef{{Name: "nas"}},
			},
			"postgres": {
				Name:         "postgres",
				Source:       config.Source{Paths: []string{"/b"}},
				Policy:       "hot",
				Repositories: []config.RepositoryRef{{Name: "nas"}},
			},
		},
	}
}

func TestExec_UndefinedRepositoryErrors(t *testing.T) {
	cfg := execTestConfig()
	_, err := Exec(context.Background(), cfg, "does-not-exist", []string{"snapshots"}, false, Options{Restic: NewResticRunner(), LockDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error for an undefined repository")
	}
}

func TestExec_GateBlocksBeforeAnyLockOrResticInvocation(t *testing.T) {
	cfg := execTestConfig()
	result, err := Exec(context.Background(), cfg, "nas", []string{"forget", "--prune"}, false, Options{Restic: NewResticRunner(), LockDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.Blocked != BlockedByGate {
		t.Fatalf("expected BlockedByGate, got %+v", result)
	}
	if result.ExitCode != ExitGateBlocked {
		t.Fatalf("expected exit code %d, got %d", ExitGateBlocked, result.ExitCode)
	}
	if len(result.SharingJobs) != 2 {
		t.Fatalf("expected both sharing jobs listed, got %v", result.SharingJobs)
	}
}

func TestExec_ForceDoesNotBypassGate(t *testing.T) {
	cfg := execTestConfig()
	result, err := Exec(context.Background(), cfg, "nas", []string{"forget", "--prune"}, true /* force */, Options{Restic: NewResticRunner(), LockDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.Blocked != BlockedByGate {
		t.Fatalf("expected force to have no effect on the gate, got %+v", result)
	}
}

func TestExec_LockHeldByAnotherExecutionBlocksAGuardedCommand(t *testing.T) {
	cfg := execTestConfig()
	lockDir := t.TempDir()

	held, ok, err := lock.AcquireRepository(lockDir, "nas")
	if err != nil || !ok {
		t.Fatalf("failed to pre-acquire lock for test setup: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	result, err := Exec(context.Background(), cfg, "nas", []string{"prune"}, false, Options{Restic: NewResticRunner(), LockDir: lockDir})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.Blocked != BlockedByLock {
		t.Fatalf("expected BlockedByLock, got %+v", result)
	}
	if result.ExitCode != ExitLockBlocked {
		t.Fatalf("expected exit code %d, got %d", ExitLockBlocked, result.ExitCode)
	}
}

func TestExec_ForceSkipsTheLockAndInvokesRestic(t *testing.T) {
	requireRestic(t)
	repo := initRepo(t)
	cfg := &config.Config{
		Repositories: map[string]config.Repository{"nas": repo},
		Policies:     map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 1}}},
		Backups: map[string]config.Job{
			"documents": {Name: "documents", Source: config.Source{Paths: []string{"/a"}}, Policy: "hot", Repositories: []config.RepositoryRef{{Name: "nas"}}},
		},
	}
	lockDir := t.TempDir()

	held, ok, err := lock.AcquireRepository(lockDir, "nas")
	if err != nil || !ok {
		t.Fatalf("failed to pre-acquire lock for test setup: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	// "prune" would normally require the exclusive lock and be deferred;
	// force should skip that guard and let restic run directly (it
	// succeeds harmlessly against a freshly-initialized, empty repo).
	result, err := Exec(context.Background(), cfg, "nas", []string{"prune"}, true, Options{Restic: NewResticRunner(), LockDir: lockDir})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.Blocked != NotBlocked {
		t.Fatalf("expected force to skip the lock guard and actually invoke restic, got %+v", result)
	}
}
