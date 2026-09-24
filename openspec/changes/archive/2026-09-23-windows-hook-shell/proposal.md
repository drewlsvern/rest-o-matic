## Why

rest-o-matic runs every hook with `sh -c` on every platform, but Windows has no `sh`. On the Windows build (which releases already ship), every `before`/`after` hook fails with `exec: "sh": executable file not found in %PATH%`, so a Windows machine can back up but can't stop services, restart them, or ping a health check. Stopping a hook on Windows also kills only the shell it was started through, leaving any program that shell started (e.g. `curl.exe`) running.

## What Changes

- On Windows, hooks run through `cmd.exe /S /C "<command>"`, passed as a raw command line so the hook string reaches `cmd` exactly as written in the config (quotes included). Linux and macOS keep `sh -c`, unchanged.
- On Windows, stopping a hook (on interrupt or cleanup timeout) kills the hook's whole process tree, not just `cmd.exe`.
- Docs: hook commands use the platform's shell syntax (`%VAR%` on Windows vs `$VAR` on Unix), with a Windows example.
- CI gains a Windows job that builds the project and runs the hook runner's tests on a Windows runner, so the Windows path is exercised on every pull request.
- No configurable shell: the shell is determined by the platform. Anyone who wants PowerShell can call it from the hook (`powershell -NoProfile -File script.ps1`).

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `backup-execution`: new requirements for which shell runs hook commands on each platform, and that stopping a hook stops the processes it started.
- `ci-build-check`: new requirement that pull requests also run the hook tests on Windows.

## Impact

- `internal/execution/hook.go`, `hook_unix.go`, `hook_windows.go`: the shell invocation moves into the platform files. The Windows file sets the raw command line and a tree-kill cancel.
- Hook tests: Windows-specific variants (cmd syntax), and Unix-only tests behind build tags.
- `.github/workflows/ci.yml`: a new `windows-latest` job.
- README and `rest-o-matic.example.yaml`: a note on platform-specific hook syntax.
- Repository settings (manual): mark the new Windows check as required on `main`'s branch protection if it should block merges.
- No change to Linux/macOS behaviour or to config format.
