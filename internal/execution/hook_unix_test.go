//go:build !windows

package execution

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunHooksStopOnError_StopsAtFirstFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	err := runHooksStopOnError(context.Background(), []string{"exit 3", "touch " + marker}, nil)
	if err == nil {
		t.Fatal("expected an error from the failing command")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("expected the command after the failure not to run")
	}
}

func TestRunHooksAll_RunsEveryCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	errs := runHooksAll(context.Background(), []string{"exit 3", "touch " + marker, "exit 4"}, nil)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("expected the command after a failure to still run")
	}
}

func TestRunHook_SeesInjectedEnv(t *testing.T) {
	err := runHook(context.Background(), `test "$RESTOMATIC_JOB" = documents`, []string{"RESTOMATIC_JOB=documents"})
	if err != nil {
		t.Fatalf("expected the hook to see RESTOMATIC_JOB: %v", err)
	}
}

func TestRunHook_CancelStopsChildProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("checks process state via /proc")
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		// The shell starts a background child and waits on it, like a hook
		// running rsync: cancelling must stop the child, not just the shell.
		done <- runHook(ctx, "sleep 30 & echo $! > "+pidFile+"; wait", nil)
	}()

	var pid string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(pidFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			pid = strings.TrimSpace(string(b))
			break
		}
	}
	if pid == "" {
		t.Fatal("child never started")
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancelled hook to report an error")
		}
	case <-time.After(hookWaitDelay + 5*time.Second):
		t.Fatal("cancelled hook did not return")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("expected SIGTERM to stop the hook promptly, took %v", elapsed)
	}
	if !processGone(pid) {
		t.Errorf("child process %s still running after cancel", pid)
	}
}

// processGone reports whether pid has exited, treating a zombie awaiting
// reaping by init as gone. It checks /proc, so it only means anything on
// Linux; the test is skipped elsewhere.
func processGone(pid string) bool {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		stat, err := os.ReadFile("/proc/" + pid + "/stat")
		if err != nil {
			return true
		}
		if f := strings.Fields(string(stat)); len(f) > 2 && f[2] == "Z" {
			return true
		}
	}
	return false
}
