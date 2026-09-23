//go:build !windows

package execution

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// configureHookProcess starts the hook in its own process group so that
// cancelling it reaches the whole tree `sh -c` started (e.g. a running
// rsync), not just the shell: SIGTERM first, then SIGKILL after
// hookWaitDelay if anything is still alive.
func configureHookProcess(cmd *exec.Cmd) {
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
}
