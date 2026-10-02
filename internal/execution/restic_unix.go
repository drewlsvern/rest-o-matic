//go:build !windows

package execution

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// configureWrappedResticCancel is configureResticCancel for restic started
// through a wrapper (podman unshare), which may not forward signals. The
// command gets its own process group and SIGINT goes to the whole group,
// so restic receives it directly and can remove its repository lock; if
// anything is still alive after resticWaitDelay the group is killed.
func configureWrappedResticCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		pgid := -cmd.Process.Pid
		time.AfterFunc(resticWaitDelay, func() { _ = syscall.Kill(pgid, syscall.SIGKILL) })
		if err := syscall.Kill(pgid, syscall.SIGINT); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.WaitDelay = resticWaitDelay
}
