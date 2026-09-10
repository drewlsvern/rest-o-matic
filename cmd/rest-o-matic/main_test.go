package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireRestic(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic binary not available on PATH; skipping integration test")
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "rest-o-matic")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building test binary: %v: %s", err, out)
	}
	return bin
}

// setupWorkspace creates a temp dir with an initialized local restic
// repository and a config file with one job ("documents") pointed at it.
func setupWorkspace(t *testing.T) (workdir, configPath string) {
	t.Helper()
	workdir = t.TempDir()
	repoPath := filepath.Join(workdir, "repo-nas")
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
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return workdir, configPath
}

func runCLI(t *testing.T, bin, workdir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = workdir
	var outBuf, errBuf outputBuf
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("running CLI: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), code
}

type outputBuf struct{ data []byte }

func (b *outputBuf) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *outputBuf) String() string { return string(b.data) }

func TestCLI_ValidateReportsMultipleErrors(t *testing.T) {
	workdir := t.TempDir()
	bin := buildBinary(t)
	configPath := filepath.Join(workdir, "bad.yaml")
	content := `
backups:
  documents:
    source: {paths: []}
    policy: missing-policy
    repositories: [missing-repo]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "validate")
	if code == 0 {
		t.Fatal("expected non-zero exit code for an invalid config")
	}
	for _, want := range []string{"undefined policy", "source.paths must be non-empty", "undefined repository"} {
		if !contains(stderr, want) {
			t.Errorf("expected stderr to mention %q, got: %s", want, stderr)
		}
	}
}

func TestCLI_TickRunsDueJobThenNoOpsOnSecondTick(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := setupWorkspace(t)

	stdout, _, code := runCLI(t, bin, workdir, "--config", configPath, "tick")
	if code != 0 {
		t.Fatalf("first tick failed (exit %d): %s", code, stdout)
	}
	if !contains(stdout, "1 job(s) due, 1 succeeded") {
		t.Fatalf("expected first tick to run the due job, got: %s", stdout)
	}

	stdout, _, code = runCLI(t, bin, workdir, "--config", configPath, "tick")
	if code != 0 {
		t.Fatalf("second tick failed (exit %d): %s", code, stdout)
	}
	if !contains(stdout, "0 job(s) due") {
		t.Fatalf("expected second tick to find nothing due, got: %s", stdout)
	}
}

func TestCLI_RunBypassesDueCheck(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := setupWorkspace(t)

	// First tick marks the job as just-run, so it would not be due again.
	if _, _, code := runCLI(t, bin, workdir, "--config", configPath, "tick"); code != 0 {
		t.Fatal("setup tick failed")
	}

	stdout, _, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	if code != 0 {
		t.Fatalf("run documents failed (exit %d): %s", code, stdout)
	}
	if !contains(stdout, "job documents: OK") {
		t.Fatalf("expected manual run to succeed despite not being due, got: %s", stdout)
	}

	// Two backups should have happened: one from tick, one from run.
	repoPath := filepath.Join(workdir, "repo-nas")
	snapCmd := exec.Command("restic", "-r", repoPath, "snapshots", "--json")
	snapCmd.Env = append(os.Environ(), "RESTIC_PASSWORD=testpass")
	out, err := snapCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restic snapshots: %v: %s", err, out)
	}
	var snaps []map[string]any
	if err := json.Unmarshal(out, &snaps); err != nil {
		t.Fatalf("parsing snapshots: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots (tick + manual run), got %d", len(snaps))
	}
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
