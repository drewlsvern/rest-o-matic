package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/execution"
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

func TestCLI_Exec_MissingPrefixRejectedBeforeRestic(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "rest-o-matic.yaml")
	content := `
repositories:
  offsite: {backend: s3, url: "host.example.com/bucket", password: x}
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// `init` is used because, were restic invoked, it would create a local
	// repository directory named after the url - the exact failure this
	// check exists to prevent.
	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "offsite", "--", "init")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d (stderr: %s)", code, stderr)
	}
	for _, want := range []string{execMessagePrefix + "config error:", `"s3:"`} {
		if !contains(stderr, want) {
			t.Errorf("expected stderr to mention %q, got: %s", want, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(workdir, "host.example.com")); !os.IsNotExist(err) {
		t.Fatalf("expected restic never to run, but a local repository directory exists (stat err: %v)", err)
	}
}

func TestCLI_Exec_UnrelatedConfigErrorsDoNotBlock(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	extra := `
  broken:
    source:
      paths: ["/a"]
    policy: missing
    repositories: [nas]
`
	workdir, configPath, _ := setupExecWorkspace(t, extra)

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nas", "--", "snapshots")
	if code != 0 {
		t.Fatalf("expected exec to run despite an unrelated job error, got exit %d (stderr: %s)", code, stderr)
	}
}

func TestCLI_Exec_WarningsPrintedThenResticRuns(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath, repoPath := setupExecWorkspace(t, "")

	// Point the repository at the same directory via a relative path, which
	// warns but still works because runCLI runs from workdir.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(string(data), "url: "+repoPath, "url: "+filepath.Base(repoPath), 1)
	if err := os.WriteFile(configPath, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "exec", "nas", "--", "snapshots")
	if code != 0 {
		t.Fatalf("expected exec to proceed despite a warning, got exit %d (stderr: %s)", code, stderr)
	}
	if !contains(stderr, execMessagePrefix+"config warning:") {
		t.Fatalf("expected a prefixed config warning on stderr, got: %s", stderr)
	}
}
