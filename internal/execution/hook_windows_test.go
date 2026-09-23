//go:build windows

package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunHooksStopOnError_StopsAtFirstFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	err := runHooksStopOnError(context.Background(), []string{"exit /b 3", `type nul > "` + marker + `"`}, nil)
	if err == nil {
		t.Fatal("expected an error from the failing command")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("expected the command after the failure not to run")
	}
}

func TestRunHooksAll_RunsEveryCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	errs := runHooksAll(context.Background(), []string{"exit /b 3", `type nul > "` + marker + `"`, "exit /b 4"}, nil)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("expected the command after a failure to still run")
	}
}

// %VAR% is only expanded by cmd.exe; a `sh` that happened to be on PATH (the
// GitHub Windows runners ship Git's) would leave it literal and fail this.
func TestRunHook_UsesCmdVariableSyntax(t *testing.T) {
	err := runHook(context.Background(), `if "%RESTOMATIC_JOB%"=="documents" (exit /b 0) else (exit /b 1)`, []string{"RESTOMATIC_JOB=documents"})
	if err != nil {
		t.Fatalf("expected cmd.exe to expand %%RESTOMATIC_JOB%%: %v", err)
	}
}

// Go's default argument escaping would turn the quotes into \" before
// cmd.exe saw them; the raw command line must deliver them untouched.
func TestRunHook_QuotesReachCmdUnchanged(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.txt")
	if err := runHook(context.Background(), `echo "a  b"> "`+out+`"`, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(string(b), "\r\n"); got != `"a  b"` {
		t.Fatalf(`expected the argument "a  b" unchanged, got %q`, got)
	}
}

func TestRunHook_CancelStopsChildProcesses(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		// cmd.exe starts PowerShell, which records its own PID and sleeps:
		// cancelling must stop PowerShell too, not just cmd.exe.
		done <- runHook(ctx, `powershell -NoProfile -Command "$PID | Out-File -Encoding ascii '`+pidFile+`'; Start-Sleep 60"`, nil)
	}()

	var pid string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if b, err := os.ReadFile(pidFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			pid = strings.TrimSpace(string(b))
			break
		}
	}
	if pid == "" {
		t.Fatal("child PowerShell never started")
	}

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancelled hook to report an error")
		}
	case <-time.After(hookWaitDelay + 10*time.Second):
		t.Fatal("cancelled hook did not return")
	}

	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if !processRunning(t, pid) {
			return
		}
	}
	t.Fatalf("child process %s still running after cancel", pid)
}

// processRunning reports whether tasklist lists a process with this PID.
func processRunning(t *testing.T, pid string) bool {
	t.Helper()
	out, err := exec.Command("tasklist", "/FI", "PID eq "+pid, "/NH").Output()
	if err != nil {
		t.Fatalf("tasklist: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[1] == pid {
			return true
		}
	}
	return false
}
