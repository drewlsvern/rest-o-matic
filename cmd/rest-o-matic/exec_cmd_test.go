package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"rest-o-matic/internal/execution"
)

// setupExecWorkspace is like setupWorkspace but lets the caller add extra
// jobs sharing the same repository, for exercising the tag-safety gate.
func setupExecWorkspace(t *testing.T, extraJobsYAML string) (workdir, configPath, repoPath string) {
	t.Helper()
	workdir = t.TempDir()
	repoPath = filepath.Join(workdir, "repo-nas")
	srcPath := filepath.Join(workdir, "src")
	if err := os.MkdirAll(srcPath, 0o755); err != nil {
		t.Fatal(err)
	}

	initCmd := exec.Command("restic", "-r", repoPath, "init")
	initCmd.Env = append(os.Environ(), "RESTIC_PASSWORD=testpass")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("restic init: %v: %s", err, out)
	}

	configPath = filepath.Join(workdir, "rest-o-matic.yaml")
	content := `
policies:
  hot:
    schedule: hourly
    retention: {hourly: 24}

repositories:
  nas:
    backend: local
    url: ` + repoPath + `
    password: testpass

backups:
  documents:
    source:
      paths: ["` + srcPath + `"]
    policy: hot
    repositories: [nas]
` + extraJobsYAML

	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return workdir, configPath, repoPath
}

func TestCLI_Exec_UndefinedRepository(t *testing.T) {
	bin := buildBinary(t)
	workdir, configPath, _ := setupExecWorkspace(t, "")

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nope", "--", "snapshots")
	if code == 0 {
		t.Fatal("expected non-zero exit for an undefined repository")
	}
	if !contains(stderr, `repository "nope" is not defined`) {
		t.Fatalf("expected an undefined-repository message, got: %s", stderr)
	}
}

func TestCLI_Exec_TransparentPassthroughMatchesDirectRestic(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath, repoPath := setupExecWorkspace(t, "")

	stdout, _, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nas", "--", "snapshots", "--json")
	if code != 0 {
		t.Fatalf("exec snapshots failed (exit %d): %s", code, stdout)
	}

	direct := exec.Command("restic", "-r", repoPath, "snapshots", "--json")
	direct.Env = append(os.Environ(), "RESTIC_PASSWORD=testpass")
	directOut, err := direct.CombinedOutput()
	if err != nil {
		t.Fatalf("direct restic snapshots: %v: %s", err, directOut)
	}

	if stdout != string(directOut) {
		t.Fatalf("exec output did not match direct restic output.\nexec:   %q\ndirect: %q", stdout, directOut)
	}
}

func TestCLI_Exec_MissingDashDashIsRejected(t *testing.T) {
	bin := buildBinary(t)
	workdir, configPath, _ := setupExecWorkspace(t, "")

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nas", "snapshots")
	if code == 0 {
		t.Fatal("expected non-zero exit when -- is missing")
	}
	if !contains(stderr, "usage:") {
		t.Fatalf("expected a usage message, got: %s", stderr)
	}
}

func TestCLI_Exec_GateBlocksUnscopedForgetOnSharedRepo(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	extra := `
  postgres:
    source:
      paths: ["` + filepath.Join(t.TempDir(), "src2") + `"]
    policy: hot
    repositories: [nas]
`
	workdir, configPath, _ := setupExecWorkspace(t, extra)

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nas", "--", "forget", "--prune")
	if code != execution.ExitGateBlocked {
		t.Fatalf("expected exit code %d, got %d (stderr: %s)", execution.ExitGateBlocked, code, stderr)
	}
	for _, want := range []string{"documents", "postgres", "cannot be bypassed"} {
		if !contains(stderr, want) {
			t.Errorf("expected stderr to mention %q, got: %s", want, stderr)
		}
	}
}

func TestCLI_Exec_ForceDoesNotBypassGateOnSharedRepo(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	extra := `
  postgres:
    source:
      paths: ["` + filepath.Join(t.TempDir(), "src2") + `"]
    policy: hot
    repositories: [nas]
`
	workdir, configPath, _ := setupExecWorkspace(t, extra)

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nas", "--force", "--", "forget", "--prune")
	if code != execution.ExitGateBlocked {
		t.Fatalf("expected --force to have no effect on the gate (exit %d), got %d (stderr: %s)", execution.ExitGateBlocked, code, stderr)
	}
}

func TestCLI_Exec_TaggedForgetOnSharedRepoSucceeds(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	extra := `
  postgres:
    source:
      paths: ["` + filepath.Join(t.TempDir(), "src2") + `"]
    policy: hot
    repositories: [nas]
`
	workdir, configPath, _ := setupExecWorkspace(t, extra)

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nas", "--", "forget", "--tag", "documents", "--keep-hourly", "24")
	if code != 0 {
		t.Fatalf("expected tagged forget to succeed, got exit %d (stderr: %s)", code, stderr)
	}
}
