//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startCLI starts the binary without waiting for it, so a test can signal it.
func startCLI(t *testing.T, bin, workdir string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = workdir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd, &out
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", path)
}

// waitExit waits for cmd to exit within limit, returning its exit code.
func waitExit(t *testing.T, cmd *exec.Cmd, limit time.Duration) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		return cmd.ProcessState.ExitCode()
	case <-time.After(limit):
		t.Fatalf("process did not exit within %v", limit)
		return -1
	}
}

func writeSignalConfig(t *testing.T, workdir, backups string) string {
	t.Helper()
	path := filepath.Join(workdir, "rest-o-matic.yaml")
	content := `
max_concurrent: 1
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: ` + filepath.Join(workdir, "never-initialized") + `, password: x}
backups:
` + backups
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLI_SIGTERMRunsCleanupAndRecordsFailure(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	started, log := filepath.Join(workdir, "started"), filepath.Join(workdir, "log")
	configPath := writeSignalConfig(t, workdir, `
  documents:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
    hooks:
      before: ["touch `+started+`; sleep 30"]
      after:
        always: ["echo always >> `+log+`"]
        success: ["echo success >> `+log+`"]
        failure: ["echo failure >> `+log+`"]
`)

	cmd, out := startCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	waitForFile(t, started)
	_ = cmd.Process.Signal(syscall.SIGTERM)

	if code := waitExit(t, cmd, 10*time.Second); code == 0 {
		t.Fatalf("expected a non-zero exit after SIGTERM, output: %s", out)
	}
	logged, _ := os.ReadFile(log)
	if got := strings.Join(strings.Fields(string(logged)), " "); got != "always failure" {
		t.Fatalf("got hook order %q, want %q (output: %s)", got, "always failure", out)
	}
	if !strings.Contains(out.String(), "interrupted by SIGTERM") {
		t.Errorf("expected output to report the interrupt, got: %s", out)
	}
	state, _ := os.ReadFile(filepath.Join(workdir, ".rest-o-matic", "state.json"))
	if !strings.Contains(string(state), `"last_outcome": "failed"`) {
		t.Errorf("expected the interrupted job to be recorded as failed, state: %s", state)
	}
}

func TestCLI_SecondSignalSkipsCleanup(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	inAlways := filepath.Join(workdir, "in-always")
	configPath := writeSignalConfig(t, workdir, `
  documents:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
    hooks:
      after:
        always: ["touch `+inAlways+`; sleep 30"]
`)

	cmd, out := startCLI(t, bin, workdir, "--config", configPath, "run", "documents")
	waitForFile(t, inAlways)
	_ = cmd.Process.Signal(syscall.SIGINT) // starts the 60s cleanup grace
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	_ = cmd.Process.Signal(syscall.SIGINT) // abandons cleanup

	waitExit(t, cmd, 10*time.Second)
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("expected a prompt exit after the second signal, took %v (output: %s)", elapsed, out)
	}
}

func TestCLI_TickDoesNotStartQueuedJobsAfterInterrupt(t *testing.T) {
	bin := buildBinary(t)
	workdir := t.TempDir()
	aStarted, bStarted := filepath.Join(workdir, "a-started"), filepath.Join(workdir, "b-started")
	configPath := writeSignalConfig(t, workdir, `
  a:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
    hooks:
      before: ["touch `+aStarted+`; sleep 30"]
  b:
    source: {paths: ["`+workdir+`"]}
    policy: hot
    repositories: [nas]
    hooks:
      before: ["touch `+bStarted+`"]
`)

	cmd, out := startCLI(t, bin, workdir, "--config", configPath, "tick")
	waitForFile(t, aStarted)
	_ = cmd.Process.Signal(syscall.SIGINT)

	if code := waitExit(t, cmd, 10*time.Second); code == 0 {
		t.Fatalf("expected a non-zero exit, output: %s", out)
	}
	if _, err := os.Stat(bStarted); err == nil {
		t.Fatalf("queued job b started after the interrupt (output: %s)", out)
	}
	if !strings.Contains(out.String(), "1 not started (interrupted by SIGINT)") {
		t.Errorf("expected the summary to report the job that didn't start, got: %s", out)
	}
}
