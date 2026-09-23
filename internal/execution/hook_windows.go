//go:build windows

package execution

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// newHookCmd runs command through cmd.exe. The command line is set raw
// because Go's argument escaping (the C-runtime rules) isn't what cmd.exe
// parses: passing command as an argument would turn every `"` into `\"`.
// With /S, cmd strips exactly the outer pair of quotes and runs the rest
// verbatim, so the hook reaches cmd exactly as written in the config.
//
// Windows has no process groups or reliable way to ask a console program to
// exit, so cancelling kills the whole tree immediately with taskkill.
func newHookCmd(ctx context.Context, command string) *exec.Cmd {
	shell := os.Getenv("ComSpec")
	if shell == "" {
		shell = "cmd.exe"
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /S /C "` + command + `"`}
	cmd.Cancel = func() error {
		// The usual failure is that the process already exited.
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		return nil
	}
	cmd.WaitDelay = hookWaitDelay
	return cmd
}
