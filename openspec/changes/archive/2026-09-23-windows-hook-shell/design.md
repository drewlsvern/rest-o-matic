## Context

- `runHook` in `internal/execution/hook.go` always builds `exec.CommandContext(ctx, "sh", "-c", command)`, then calls the platform's `configureHookProcess`.
- `hook_unix.go` (`!windows`) puts the hook in its own process group and cancels it with SIGTERM to the group, then SIGKILL after `hookWaitDelay`.
- `hook_windows.go` only sets `WaitDelay`, so cancelling uses exec's default `Process.Kill`, which kills the direct child only.
- Hook and job tests use `sh` syntax (`exit 3`, `touch`, `test "$X" = y`, `sleep`, `&`). CI runs only on `ubuntu-latest`.
- Releases already ship a `windows/amd64` binary.

See proposal.md for motivation and specs/ for exact behaviour.

## Goals / Non-Goals

**Goals:**
- Hooks work on a stock Windows install, with the command string reaching the shell exactly as written.
- Stopping a hook on Windows stops everything it started.
- The Windows path is exercised by CI.

**Non-Goals:**
- A configurable shell, or PowerShell as the default.
- Making the full test suite pass on Windows (most of it assumes `sh` and restic). Only the hook runner is tested there.
- Graceful (SIGTERM-like) stopping on Windows; see Decisions.
- Any change to Linux/macOS behaviour.

## Decisions

### Platform files build the whole hook command
`runHook` calls `newHookCmd(ctx, command) *exec.Cmd` instead of building `sh -c` itself. Each platform file owns both the shell invocation and its cancellation, so the two can't drift apart:

- `hook_unix.go`: `exec.CommandContext(ctx, "sh", "-c", command)` plus today's process-group setup (moved, unchanged).
- `hook_windows.go`: the `cmd.exe` invocation below, plus tree-kill cancellation.

`runHook` keeps setting the environment, capturing output and building the error.

### Windows: raw command line via `SysProcAttr.CmdLine`
```go
shell := os.Getenv("ComSpec") // normally C:\Windows\System32\cmd.exe
if shell == "" { shell = "cmd.exe" }
cmd := exec.CommandContext(ctx, shell)
cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /S /C "` + command + `"`}
```

Go escapes arguments with the C-runtime (`CommandLineToArgvW`) rules, which `cmd.exe` doesn't use. `exec.Command("cmd", "/C", command)` would turn every `"` in a hook into `\"`, which cmd passes on literally. The `os/exec` docs recommend setting `SysProcAttr.CmdLine` directly for `cmd`. `/S` makes cmd strip exactly the outer pair of quotes and run the rest verbatim, whatever quotes the command contains. `ComSpec` is the standard way to locate cmd, and avoids relying on `PATH`.

Alternative rejected: `exec.Command("cmd", "/C", command)`, because of the quoting above.

### Windows: stop the tree with `taskkill /T /F`
```go
cmd.Cancel = func() error {
    _ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
    return nil
}
cmd.WaitDelay = hookWaitDelay
```

`/T` kills the process and every descendant, and `/F` forces it. `taskkill` ships with every Windows version rest-o-matic supports. Its error is ignored, like `ESRCH` on Unix: the usual failure is "already exited". `WaitDelay` still backs it up by killing the direct child and closing the output pipes if anything is left.

Alternative considered: Windows job objects (assign the hook to a job, terminate the job). They're more robust, since they also catch processes that detach from the tree, but they need `golang.org/x/sys/windows` and a noticeable amount of Win32 code. `taskkill` covers the realistic cases (a hook running `curl`, `robocopy` or `powershell`) in a few lines.

No graceful stop: console programs on Windows can't reliably be sent a Ctrl-C/Ctrl-Break from a non-parent process without detaching consoles, so hooks are killed immediately. This is documented; the Unix behaviour (SIGTERM first) is unchanged.

### Tests split by platform
- The existing hook runner tests move to `hook_unix_test.go` (`//go:build !windows`), unchanged.
- New `hook_windows_test.go` (`//go:build windows`) covers the same behaviours in cmd syntax:
  - stop-on-error vs run-all (`exit /b 3`, `type nul > file`);
  - an env var via `if "%RESTOMATIC_JOB%"=="documents"`;
  - a quoted argument passed through intact (echo it to a file and compare);
  - tree kill: the hook starts `powershell -NoProfile -Command "$PID | Out-File …; Start-Sleep 60"`, and after cancel `tasklist /FI "PID eq <pid>"` must not list it.
- The `%RESTOMATIC_JOB%` test also proves `cmd` is being used, not a `sh` that happens to be on `PATH`: GitHub's Windows runners ship Git's `sh`, which wouldn't expand `%VAR%`.
- `job_hooks_test.go` and the other `sh`/restic-based tests in the package get `//go:build !windows`, so the package compiles and runs cleanly on Windows. `signal_cmd_test.go` is already Unix-only.

### CI job
A second job in `ci.yml`, on the same `pull_request` trigger:

```yaml
windows:
  runs-on: windows-latest
  steps: checkout → setup-go (go.mod) → go build ./... → go vet ./... → go test ./internal/execution/
```

Once the Unix-only tests carry build tags, the whole `internal/execution` package runs, not just a `-run` filter, so a new Windows test can't be missed by a name pattern. Making the check required is a branch-protection setting, done by hand.

## Risks / Trade-offs

- **[Risk] cmd treats `%`, `^`, `&`, `|`, `<`, `>` specially.** A URL containing `%20` could be changed if an environment variable with that name exists, and cmd `/C` has no clean way to escape `%`. → That's how cmd behaves, and it's documented as "hooks use the platform shell's syntax". Complex Windows hooks belong in a `.cmd` or `.ps1` script called from the hook.
- **[Risk] `taskkill /T` misses processes that detach from the tree** (e.g. started via `start` in a new window, or by a service). → Rare in hooks. Job objects remain the upgrade path if it matters.
- **[Trade-off] No graceful stop on Windows.** → A Windows hook interrupted mid-run doesn't get to clean up. Acceptable for the one-liners hooks usually are.
- **[Trade-off] A Windows CI job adds a minute or two per PR.** → Free for this public repository. It runs in parallel with the Linux job.
- **[Risk] The config isn't portable across OSes** if hooks use shell-specific syntax. → Documented. Simple commands (like a `curl` ping) work on both.

## Migration Plan

- Linux/macOS: nothing changes.
- Windows: hooks that failed with `exec: "sh": executable file not found` start working. Hooks written for Git's `sh` (e.g. using `$VAR`) must be rewritten in cmd syntax, or wrapped as `sh -c '…'` if Git's `sh` is installed.
- After merging: add the new Windows check to `main`'s required status checks if it should block merges.
