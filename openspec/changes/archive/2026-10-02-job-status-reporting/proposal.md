## Why

Checking whether backups are working on a host means reading cron mail or running restic commands by hand. rest-o-matic records only the time and outcome of each job's last run, so it can't say why a job failed, how long it took, when it will next run, or whether the scheduler is still firing.

This is the first step of the central-management design ([docs/design/central-management.md](../../../../docs/design/central-management.md)). The report a host will later send to the central app is this same information, so it has to exist locally first. It's also useful by itself on a single host.

It depends on the `fix-job-concurrency` change, which adds the per-job lock this change uses to show a job as running. That change must be merged first.

## What Changes

- A new `status` command shows every configured job: schedule, last run, outcome, duration, next due time, and whether it is running now. A failed job shows its error and which repository failed.
- `status <job>` shows that job's recent runs.
- `status --json` prints the same information as one JSON document, for scripts and for the later central-management work.
- Each run's record gains its start and finish time, how it was started (`tick` or `run`), a one-line error, and a result for each repository including the snapshot ID.
- The state file keeps the last 20 runs of each job.
- Each `tick` records when it ran, so `status` can show whether the scheduler is still firing.
- Updates to the state file are made safe across processes, so two executions finishing at the same moment can't lose one of the records.
- A failure to write the state file is reported as a warning. Today it is silently ignored.
- State files written by earlier versions keep working unchanged.

## Capabilities

### New Capabilities
- `job-status`: the `status` command, what it reports for each job, and its JSON form.

### Modified Capabilities
- `scheduling`: adds what is recorded about each run, the bounded run history, the recorded tick time, and safe concurrent updates to the state file.

## Impact

- `cmd/rest-o-matic`: a new `status` command; `tick` and `run` record the tick time and write the richer run record.
- `internal/state`: a larger state format, the run history, and a cross-process lock around updates.
- `internal/lock`: a way to test whether a job's lock is held, and a short retry when taking it.
- `internal/schedule`: a function giving a job's next due time.
- `internal/execution`: per-repository results carry the snapshot ID, which is currently discarded.
- One new file in `<state-dir>/locks/` (`state.lock`), covered by the existing owner check without changes.
- README: a new "Status" section.
- No config format change. No new dependencies.
