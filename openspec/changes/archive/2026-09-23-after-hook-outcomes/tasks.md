## 1. Config Shape

- [x] 1.1 Add `AfterHooks{Always, Success, Failure []string}` and change `Hooks.After` to use it
- [x] 1.2 Implement `AfterHooks.UnmarshalYAML`: sequence → `Always`; mapping → walk key/value pairs, reject unknown keys with the line number and accepted keys; null → empty; anything else → error
- [x] 1.3 Unit tests for every scenario in `specs/config/spec.md`: block and inline list form, full map, partial map, inline map, unknown key (error mentions key and line), bare string, empty `after:`

## 2. Hook Runner

- [x] 2.1 Extract `runHook(ctx, command, env)` running one `sh -c` command with `os.Environ()` plus the given variables
- [x] 2.2 Unix (`//go:build !windows`): start hooks in their own process group, `cmd.Cancel` sends SIGTERM to the group, `WaitDelay` 5s; Windows file: `Process.Kill` fallback
- [x] 2.3 Add `runHooksStopOnError` (for `before`, today's behaviour) and `runHooksAll` (runs every command, returns all errors)
- [x] 2.4 Unit tests: stop-on-error stops after the first failure; run-all runs every command and returns each error; a hook sees an injected env var; cancelling the context stops a hook and its child (e.g. `sh -c 'sleep 30 & wait'`)

## 3. Restic Cancellation

- [x] 3.1 In `ResticRunner.Backup` and `Forget`, set `cmd.Cancel` to send `os.Interrupt` and `cmd.WaitDelay` to 10s so a cancelled restic removes its lock
- [x] 3.2 Test with a fake restic binary (the existing test pattern, or a script) that a cancelled context delivers SIGINT, not SIGKILL

## 4. Job Execution

- [x] 4.1 Extend `JobResult` with `AlwaysErrs`, `OutcomeHookErrs`, `Interrupted`; update `Success()` to require no `AlwaysErrs` and not `Interrupted`
- [x] 4.2 Change `RunJob` to take a work context and a cleanup context; run `before` (stop-on-error, work ctx) → repositories (skipped once the work ctx is cancelled) → `always` → `success` or `failure`, with post-backup hooks under the cleanup ctx, time-limited by the cleanup grace only if the work ctx was cancelled
- [x] 4.3 Build the hook environment: `RESTOMATIC_JOB` for all hooks; `RESTOMATIC_OUTCOME`, `RESTOMATIC_FAILED_REPOS` (config order, deferred included) and `RESTOMATIC_ERROR` (first failure, one line, ≤500 bytes, `interrupted by <signal>` on interrupt) for outcome hooks
- [x] 4.4 Make the cleanup grace an internal variable (default 60s) that tests can shorten
- [x] 4.5 Update existing `RunJob` callers and tests for the new signature
- [x] 4.6 Job tests covering every `backup-execution` delta scenario: order with key order reversed, before failure → always then failure, failed always → failure and job failed, one failed repository → failure, failing success hook leaves outcome successful, run-all for always, env vars seen by failure and before hooks

## 5. Signals and Commands

- [x] 5.1 Add a two-stage signal helper in `cmd/rest-o-matic`: first SIGINT/SIGTERM cancels the work context, second cancels the cleanup context
- [x] 5.2 Use it in `run` and `tick`; pass both contexts through `executeWithSlot`/`executeAndRecord` into `RunJob`; record interrupted jobs as failed
- [x] 5.3 `tick.dispatch`: check the work context before and after acquiring the semaphore, and skip jobs that haven't started
- [x] 5.4 `printResult`: report `always` failures (they affect the outcome) and `success`/`failure` hook failures (informational)
- [x] 5.5 CLI tests with the built binary:
  - SIGTERM during a sleeping `before` runs `always` and `failure`, exits non-zero, and records the job as failed
  - a second SIGINT during a sleeping `always` exits promptly
  - a hung `always` after an interrupt is stopped once the (shortened) grace expires. *Covered by `TestRunJob_CleanupBoundedAfterInterrupt` at the `RunJob` level instead: the binary's 60s grace can't be shortened without a test-only setting.*
  - a queued `tick` job doesn't start after SIGINT

## 6. Docs and Verification

- [x] 6.1 `rest-o-matic.example.yaml` and README: document the list and map forms of `after`, the run order, the env vars, interrupt behaviour, and the systemd `TimeoutStopSec` note
- [x] 6.2 `go test ./...` and `go vet ./...` pass; `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` succeed
- [x] 6.3 Your existing config (`~/rest-o-matic.yaml`) still validates unchanged
- [x] 6.4 `openspec validate after-hook-outcomes --strict` passes
