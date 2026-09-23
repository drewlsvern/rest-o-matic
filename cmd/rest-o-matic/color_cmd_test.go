package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	escRed    = "\x1b[31m"
	escOrange = "\x1b[38;5;208m"
	escGreen  = "\x1b[32m"
	escReset  = "\x1b[0m"
)

// writeMixedConfig writes a config with one repository error (s3 url missing
// its prefix) and one repository warning (relative local path).
func writeMixedConfig(t *testing.T, workdir string) string {
	t.Helper()
	configPath := filepath.Join(workdir, "mixed.yaml")
	content := `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  bad: {backend: s3, url: "host.example.com/bucket", password: x}
  rel: {backend: local, url: rel/repo, password: x}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [bad, rel]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func TestCLI_Color_AlwaysColoursConfigLabels(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeMixedConfig(t, workdir)

	_, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "--color=always", "validate")
	if code == 0 {
		t.Fatal("expected validate to fail on the s3 repository")
	}
	for _, want := range []string{
		escRed + "config error:" + escReset + " ",
		escOrange + "config warning:" + escReset + " ",
		escRed + "Error:" + escReset + " ",
	} {
		if !contains(stderr, want) {
			t.Errorf("expected stderr to contain %q, got: %q", want, stderr)
		}
	}
}

func TestCLI_Color_NoColorRespectedUnlessForced(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := writeMixedConfig(t, workdir)
	t.Setenv("NO_COLOR", "1")

	_, stderr, _ := runCLI(t, bin, workdir, "--config", configPath, "--color=always", "validate")
	if !contains(stderr, escRed+"config error:") {
		t.Errorf("expected --color=always to override NO_COLOR, got: %q", stderr)
	}

	_, stderr, _ = runCLI(t, bin, workdir, "--config", configPath, "validate")
	if contains(stderr, "\x1b[") {
		t.Errorf("expected no escape sequences with NO_COLOR and the default mode, got: %q", stderr)
	}
}

func TestCLI_Color_InvalidValueRejectedBeforeRunning(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "ok.yaml")
	content := `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /tmp/repo, password: x}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [nas]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "--color=sometimes", "validate")
	if code == 0 {
		t.Fatal("expected a non-zero exit for an invalid --color value")
	}
	if !contains(stderr, "auto, always, or never") {
		t.Errorf("expected the error to name the accepted values, got: %s", stderr)
	}
	if contains(stdout, "config is valid") {
		t.Errorf("expected validate not to run, got stdout: %s", stdout)
	}
}

func TestCLI_Color_ExecPassthroughUnchanged(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath, repoPath := setupExecWorkspace(t, "")

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "--color=always", "exec", "nas", "--", "snapshots", "--json")
	if code != 0 {
		t.Fatalf("exec snapshots failed (exit %d): %s", code, stderr)
	}

	direct := exec.Command("restic", "-r", repoPath, "snapshots", "--json")
	direct.Env = append(os.Environ(), "RESTIC_PASSWORD=testpass")
	directOut, err := direct.Output()
	if err != nil {
		t.Fatalf("direct restic snapshots: %v", err)
	}
	if stdout != string(directOut) {
		t.Fatalf("exec output changed under --color=always.\nexec:   %q\ndirect: %q", stdout, directOut)
	}
}

func TestCLI_Color_RunColoursSuccess(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := setupWorkspace(t)

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "--color=always", "run", "documents")
	if code != 0 {
		t.Fatalf("run failed (exit %d): %s", code, stderr)
	}
	for _, want := range []string{
		"job documents: " + escGreen + "OK" + escReset,
		"repository nas: " + escGreen + "ok" + escReset,
	} {
		if !contains(stdout, want) {
			t.Errorf("expected stdout to contain %q, got: %q", want, stdout)
		}
	}
}
