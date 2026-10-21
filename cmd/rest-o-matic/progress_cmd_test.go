//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// progressFile is where a job's progress record is in a test workspace.
func progressFile(workdir, job string) string {
	return filepath.Join(workdir, ".rest-o-matic", "progress", job+".json")
}

func TestCLI_ProgressRecordedWhileRunningAndRemovedOnInterrupt(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	started := filepath.Join(workdir, "started")
	configPath := writeSignalConfig(t, workdir, `
  documents:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
    hooks:
      before: ["touch `+started+`; sleep 30"]
`)
	cmd, out := startCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	waitForFile(t, started)
	waitForFile(t, progressFile(workdir, "documents"))

	data, err := os.ReadFile(progressFile(workdir, "documents"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct{ Phase string }
	if err := json.Unmarshal(data, &p); err != nil || p.Phase != "before_hooks" {
		t.Fatalf("progress while in the before hook: %s (%v)", data, err)
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)
	waitExit(t, cmd, 10*time.Second)
	if _, err := os.Stat(progressFile(workdir, "documents")); !os.IsNotExist(err) {
		t.Fatalf("progress record left after an interrupted run: %v (output: %s)", err, out)
	}
}

func TestCLI_ProgressRemovedAfterSuccessAndFailure(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)

	workdir, configPath := setupWorkspace(t)
	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	if code != 0 {
		t.Fatalf("run failed: %s", stderr)
	}
	// Progress goes to stderr, as plain lines when it isn't a terminal;
	// stdout holds the result lines only, as before.
	if stdout != "job documents: OK\n  repository nas: ok\n" {
		t.Errorf("stdout changed: %q", stdout)
	}
	if !strings.Contains(stderr, " documents: backing up to nas (1 of 1)\n") || !strings.Contains(stderr, " documents: applying retention on nas (1 of 1)\n") || strings.Contains(stderr, "\r") {
		t.Errorf("stderr lacks the plain phase lines: %q", stderr)
	}
	if _, err := os.Stat(progressFile(workdir, "documents")); !os.IsNotExist(err) {
		t.Fatalf("progress record left after a successful run: %v", err)
	}

	failing := t.TempDir()
	failingConfig := writeSignalConfig(t, failing, `
  documents:
    source: {paths: ["`+failing+`"]}
    policy: hot
    repositories: [nas]
`)
	if _, _, code := runCLI(t, bin, failing, "--config", failingConfig, "run", "documents"); code == 0 {
		t.Fatal("expected the run against an uninitialised repository to fail")
	}
	if _, err := os.Stat(progressFile(failing, "documents")); !os.IsNotExist(err) {
		t.Fatalf("progress record left after a failed run: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(progressFile(failing, "documents"))); err != nil {
		t.Fatalf("expected the progress directory to have been used: %v", err)
	}
}
