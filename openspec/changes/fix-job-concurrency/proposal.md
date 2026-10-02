## Why

The `concurrency-control` spec says a job that can't start yet is queued and runs as soon as it can, and that a running job is never started twice. The code does neither. It takes a repository's lock only after a job's `before` hooks have run, and gives up at once if the lock is held. Three defects follow, two of them reproduced on 2026-10-01 (output in design.md):

1. **A running job is started again by the next `tick`.** The second execution runs the `before` hooks, finds the repository locked, runs the `after` hooks and reports a failure. With a five-minute cron entry, a backup longer than five minutes has its container started again mid-backup, and any `failure` hook sends a false alert.
2. **Two jobs that share a repository and are due together lose a backup.** One is "deferred", recorded as failed, and not retried until its next schedule boundary. Jobs on the same schedule are always due together, so a daily job loses its backup every day.
3. **A job that finds no free concurrency slot is reported as failed**, although it simply runs on a later tick.

All three have one cause and one fix, so they are handled together.

## What Changes

- Before a job runs anything, it now obtains everything it needs, in this order: its own job lock, the lock of every repository it backs up to, and a concurrency slot. Only then do its `before` hooks run.
- A job that finds a repository or all slots in use **waits**, and starts as soon as they are free, without needing another `tick`. Nothing of the job runs while it waits, so a container is not stopped until the backup can actually begin.
- A job that is already running or already waiting is not started again. `tick` skips it: no hooks, nothing recorded, not counted as failed, exit zero. `run` refuses it with an error.
- Waiting is not a failure. A job that waited and then ran is reported by its real outcome.
- A job interrupted while waiting has not started: none of its hooks run and nothing is recorded.
- If the process running or waiting for a job is killed, its locks are released and nothing needs cleaning up.
- **BREAKING** (behavioural): a repository is no longer reported as "deferred" within a job run. `tick` and `run` may now stay running while they wait for another job to finish, where before they failed immediately.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `concurrency-control`: "FIFO Queuing of Deferred Jobs" gains what waiting means (nothing runs, not a failure, works across processes, interruption); "Cross-Tick Running-Job Detection" gains what must not happen for a job already running or waiting, covers `run`, and requires that a killed execution doesn't block later ones.
- `backup-execution`: "Job Outcome Selects Success or Failure Hooks" and "Hook Environment" drop their mentions of a deferred repository, which can no longer occur.

## Impact

- `internal/lock`: a per-job lock, and waiting variants of the repository and slot locks.
- `internal/execution`: `runRepo` no longer takes the repository lock; `RepoOutcome.Deferred` is removed.
- `cmd/rest-o-matic`: `executeWithSlot` becomes the single place a job acquires what it needs; `tick` and `run` handle "already running" and print a line when a job is waiting.
- `exec` is unchanged: it still refuses at once (exit 20) when a repository is in use.
- One new kind of file in `<state-dir>/locks/` (`job-<name>.lock`). The owner check in the open `state-dir-owner-guard` change covers every file in that directory, so it needs no change.
- README: the "Concurrency" section is rewritten to describe waiting and the third rule.
- No config format change, no state format change, no new dependencies.
- The planned `job-status-reporting` change builds on the job lock to show a job as running or waiting.
