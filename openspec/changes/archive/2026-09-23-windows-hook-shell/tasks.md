## 1. Platform Hook Command

- [x] 1.1 Change `runHook` to get its `*exec.Cmd` from a platform `newHookCmd(ctx, command)` and drop the shared `sh -c` construction
- [x] 1.2 `hook_unix.go`: `newHookCmd` builds `sh -c` and applies today's process-group setup and SIGTERM/SIGKILL cancel (moved, behaviour unchanged)
- [x] 1.3 `hook_windows.go`: `newHookCmd` runs `%ComSpec%` (fallback `cmd.exe`) with `SysProcAttr.CmdLine = cmd.exe /S /C "<command>"`, `Cancel` running `taskkill /T /F /PID <pid>` (error ignored), and `WaitDelay = hookWaitDelay`
- [x] 1.4 `GOOS=windows go build ./...` and `GOOS=windows go vet ./...` succeed; Linux tests unchanged and passing

## 2. Tests

- [x] 2.1 Move the existing hook runner tests to `hook_unix_test.go` with `//go:build !windows`
- [x] 2.2 Add `//go:build !windows` to the other test files in `internal/execution` that depend on `sh` or restic (`job_hooks_test.go`, `job_test.go`, `exec_test.go`, `restic_test.go` as needed) so the package's tests compile and run cleanly on Windows
- [x] 2.3 Add `hook_windows_test.go` (`//go:build windows`): stop-on-error vs run-all in cmd syntax; `%RESTOMATIC_JOB%` expansion (proves cmd, not `sh`); a double-quoted argument with spaces passed through unchanged; cancelling a hook that started `powershell … Start-Sleep 60` leaves no process with that PID
- [x] 2.4 Verify locally that `GOOS=windows go vet ./internal/execution/` compiles the Windows test file

## 3. CI

- [x] 3.1 Add a `windows` job to `.github/workflows/ci.yml` on `windows-latest`: checkout, setup-go from `go.mod`, `go build ./...`, `go vet ./...`, `go test ./internal/execution/`
- [x] 3.2 Confirm on the pull request that both the Linux and Windows jobs run and pass

## 4. Docs and Verification

- [x] 4.1 README "Hooks" section and `rest-o-matic.example.yaml`: hooks run through `sh -c` on Linux/macOS and `cmd.exe` on Windows; use that shell's syntax (`%VAR%` vs `$VAR`); a Windows example (the `curl` ping); on Windows interrupted hooks are killed immediately
- [x] 4.2 `go test ./...` and `go vet ./...` pass on Linux; `GOOS=darwin go build ./...` succeeds
- [x] 4.3 `openspec validate windows-hook-shell --strict` passes
- [x] 4.4 After merge (manual, repository owner): add the Windows check to `main`'s required status checks if it should gate merges
