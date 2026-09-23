//go:build !windows

package execution

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// newHookCmd runs command through `sh -c`, in its own process group so that
// cancelling it reaches the whole tree the shell started (e.g. a running
// rsync), not just the shell: SIGTERM first, then SIGKILL after
// hookWaitDelay if anything is still alive.
func newHookCmd(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		pgid := -cmd.Process.Pid
		time.AfterFunc(hookWaitDelay, func() { _ = syscall.Kill(pgid, syscall.SIGKILL) })
		if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.WaitDelay = hookWaitDelay
	return cmd
}
