## Context

- `config.Hooks{Before, After []string}`. `RunJob` (`internal/execution/job.go`) runs `before`, then each repository, then `after`, discarding `after` errors. `runHooks` runs `sh -c` per command and stops at the first failure.
- `JobResult.Success()` looks only at `HookErr` (a `before` failure) and per-repository outcomes.
- `run` and `tick` call `RunJob` with `context.Background()`. There is no signal handling anywhere, so SIGTERM or SIGINT kills the process at once.
- restic and hooks are started with `exec.CommandContext`, whose default cancel behaviour is SIGKILL to the direct child only.
- `tick.dispatch` starts jobs concurrently behind a semaphore; a job waiting on the semaphore hasn't started.
- Releases target Linux, macOS and Windows (`.goreleaser.yaml`).
- The dual-shape YAML precedent is `RepositoryRef.UnmarshalYAML` in `internal/config/types.go`.

See proposal.md for motivation and specs/ for exact behaviour.

## Goals / Non-Goals

**Goals:**
- A cleanup hook that runs on every outcome, including an interrupt, and a way to react to success or failure.
- No change needed to existing configs.

**Non-Goals:**
- Per-hook or per-job timeouts during normal (uninterrupted) runs. A hung `before` still hangs, as today. Follow-up.
- Surviving SIGKILL, OOM or a reboot. Nothing in-process can; that remains the backup-ctl dead-man's-switch idea.
- Retrying failed or interrupted jobs sooner than their next schedule period (the `last_run` side finding). Separate.
- Signal handling for `exec`. restic receives terminal signals directly, and exec runs no hooks.
- Making `sh -c` hooks work on Windows (they don't today).

## Decisions

### `AfterHooks` type with a custom `UnmarshalYAML`
`Hooks.After` becomes `AfterHooks{Always, Success, Failure []string}`, unmarshalled by node kind, following `RepositoryRef`:

- **SequenceNode:** decode into `Always`.
- **MappingNode:** walk `value.Content` in key/value pairs, switching on the key. An unknown key returns an error that includes `node.Line` and the three accepted keys. `Decode` with `KnownFields` isn't available per node in yaml.v3, so the keys are checked by hand.
- **Null** (`after:` with nothing after it): leave empty, matching how an empty `after:` behaves today.
- **Anything else:** error.

The error can't name the job (the unmarshaller doesn't know it), so it gives the line number, which yaml.v3 errors already use.

### Run order and outcome in `RunJob`

```
before (stop at first failure) ──fail──┐
   │ ok                                │
repos (skipped if interrupted)         │
   │                                   ▼
   └────────────────▶ always (run all) ──▶ outcome = before ok && repos ok && always ok && !interrupted
                                            │
                                  success (run all)  |  failure (run all)
```

`JobResult` gains `AlwaysErrs []error`, `OutcomeHookErrs []error` and `Interrupted bool`. `Success()` adds `len(AlwaysErrs) == 0 && !Interrupted`. `OutcomeHookErrs` are reported by `printResult` but never read by `Success()`.

### Two runners: `runHooksStopOnError` and `runHooksAll`
`before` keeps today's stop-at-first behaviour (one command may depend on the previous one, e.g. `pg_dump` then `rsync`). The post-backup lists use run-all and collect every error. Both share one `runHook(ctx, cmd, env)` for a single command.

### Environment
`cmd.Env = append(os.Environ(), vars...)`:
- Every hook gets `RESTOMATIC_JOB`.
- `success`/`failure` also get `RESTOMATIC_OUTCOME`, `RESTOMATIC_FAILED_REPOS` (repositories whose outcome isn't ok, deferred included, in config order) and `RESTOMATIC_ERROR`.
- `RESTOMATIC_ERROR` is the first failure's message, flattened to one line (newlines become spaces) and truncated to 500 bytes, so restic's multi-line stderr doesn't break shell scripts. On an interrupt it's `interrupted by <signal>`.

### Two-stage signal handling in `cmd`
A helper creates two contexts from `context.Background()`:

```
work    ── cancelled on the 1st SIGTERM/SIGINT  → stops before/restic/queued jobs
cleanup ── cancelled on the 2nd signal          → stops always/success/failure
```

A goroutine on `signal.Notify(ch, SIGINT, SIGTERM)` cancels `work` on the first signal and `cleanup` on the second. `signal.NotifyContext` alone can't do this, because it only gives one cancellation.

`RunJob(workCtx, cleanupCtx, …)`. The post-backup hooks run under a context derived from `cleanupCtx`. A `context.AfterFunc` on `workCtx` cancels it `cleanupGrace` after the interrupt, so the limit also applies when the signal arrives while the hooks are already running. Without an interrupt there's no limit, so normal-run behaviour is unchanged. A job counts as interrupted only if `workCtx` was cancelled before its backups finished; a signal that arrives during `always` only starts the grace timer.

**`cleanupGrace` = 60s, fixed.** systemd's default `TimeoutStopSec` is 90s, after which it sends SIGKILL, so 60s leaves room for rest-o-matic to finish and record state before that happens. Documented as such; configurable later if needed.

### Stopping children properly
The default `CommandContext` cancel (SIGKILL to the direct child) is wrong for both kinds of child:

- **restic:** SIGKILL leaves a stale lock in the repository. Set `cmd.Cancel` to send `os.Interrupt` (restic cleans up its lock on SIGINT) and `cmd.WaitDelay = 10s`, after which Go escalates to SIGKILL.
- **Hooks (`sh -c`):** killing `sh` orphans its children (e.g. a running `rsync`). On Unix, start each hook in its own process group (`SysProcAttr.Setpgid`), and have `cmd.Cancel` send SIGTERM to the whole group (`syscall.Kill(-pid, SIGTERM)`), with `WaitDelay = 5s` before SIGKILL. The Windows build (`//go:build windows`) falls back to `Process.Kill`.

Because hooks are in their own process group, a terminal Ctrl-C no longer reaches them directly. That's intended: rest-o-matic decides when to stop them, so `always` can still run.

### `tick` stops starting jobs after an interrupt
`dispatch` checks `workCtx` before waiting on the semaphore and again after acquiring it. A job that hasn't started is skipped entirely: no hooks, no state write, and it isn't counted in the summary.

### Recording interrupted jobs
An interrupted job is recorded with `last_outcome: failed`, as any failure is today, before the process exits. The process exits non-zero (the existing failed-job error path).

## Risks / Trade-offs

- **[BREAKING, behavioural] Jobs with failing `after` commands start failing.** Previously they were reported as successes. → Intended: a container that didn't restart is a failed job. Called out in the commit and release notes.
- **[Risk] A 60s grace may be too short for slow cleanup** (e.g. a large container start). → Most restarts take seconds. If it's hit, the command is stopped and reported, so the failure is visible. It can be made configurable later.
- **[Risk] A systemd unit with `TimeoutStopSec` below 60s still SIGKILLs mid-cleanup.** → Document it: `TimeoutStopSec` should exceed 60s.
- **[Risk] Process-group handling only on Unix.** → Windows can't run the `sh -c` hooks anyway; the fallback there is a plain kill.
- **[Trade-off] A second Ctrl-C abandons cleanup.** → That's the standard escape hatch for a person at a terminal. Under systemd, only one SIGTERM is sent.
- **[Risk] Signal handling needs real-process tests,** which can be slow or flaky. → Use short sleeps in hook scripts and send signals to the built binary. Keep the grace period injectable internally so tests don't wait 60s.

## Migration Plan

- Existing `after: [...]` configs are unchanged.
- After upgrading, check `rest-o-matic run <job>` for any job whose `after` commands were silently failing; they'll now be reported.
- systemd users: set `TimeoutStopSec=` above 60s (e.g. `120`) on the unit that runs `tick`.
