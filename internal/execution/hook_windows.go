//go:build windows

package execution

import "os/exec"

// configureHookProcess keeps exec's default cancellation (killing the
// process) on Windows, which has no process groups to signal.
func configureHookProcess(cmd *exec.Cmd) {
	cmd.WaitDelay = hookWaitDelay
}
