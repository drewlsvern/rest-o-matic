## Context

- The state file holds, for each job, `last_run` and `last_outcome` and nothing else ([state.go](../../../internal/state/state.go)). `last_run` is the time the job finished.
- A run is recorded in one place, `executeAndRecord` ([exec.go](../../../cmd/rest-o-matic/exec.go)). The error from writing state is discarded.
- `Store.Update` reads the file, changes one job and writes it back. A mutex guards this inside one process. Nothing guards it between processes, so a `tick` and a `run` finishing together can lose one record.
- `execution.JobResult` already holds everything a richer record needs except the snapshot ID, which `runRepo` discards ([job.go](../../../internal/execution/job.go)). `firstFailure` and `oneLine` already produce the one-line error used for `RESTOMATIC_ERROR`.
- `schedule.Due` answers only yes or no. There is no function for when a job is next due.
- Cross-process locks are `flock` files in `<state-dir>/locks/`, released by the kernel when the holder dies ([lock.go](../../../internal/lock/lock.go)). The owner check covers every regular file in that directory ([statedir.go](../../../internal/statedir/statedir.go)).

- The `fix-job-concurrency` change adds a per-job lock, `<state-dir>/locks/job-<name>.lock`, taken before a job waits for its repositories and a slot, and held until its state is written. This design assumes that change is merged.

### Noticed and left alone

- The `scheduling` spec says due-ness is relative to a job's last *successful* run. The code uses the last run of any outcome, so a failed daily job is not retried until the next day. `status` reports what `tick` actually does.
- Due-ness uses the finish time. A daily job that starts at 23:58 and finishes at 00:03 counts as having run on the second day.

Both are existing behaviour and are not changed by this design.

## Goals / Non-Goals

**Goals:**
- One command that answers "are this host's backups working?" without reading logs.
- A JSON form stable enough for the later check-in report to reuse.
- Existing state files and existing cron entries need no change.

**Non-Goals:**
- Anything over the network. Reporting to a central app is a later change.
- Listing snapshots from the repository. `status` reads local files only, so it stays fast and works when a backend is unreachable.
- Judging health. `status` reports facts (outcome, due, running, last tick); it does not decide what counts as "overdue", because it does not know the cron interval.
- A non-zero exit status for unhealthy jobs. Scripts use `--json`.
- Saying which process holds a repository lock (the planned enhancement noted in the README).
- Changing how due-ness is computed.

## Decisions

### State format: add to it, don't replace it
`last_run` and `last_outcome` stay exactly as they are, and still drive due-ness. New fields sit beside them:

```json
{
  "last_tick": "2026-10-01T19:03:00Z",
  "jobs": {
    "gitea": {
      "last_run": "2026-10-01T17:01:12Z",
      "last_outcome": "failed",
      "running": {"started": "2026-10-01T19:03:01Z", "trigger": "tick"},
      "runs": [
        {
          "started": "2026-10-01T17:00:00Z",
          "finished": "2026-10-01T17:01:12Z",
          "outcome": "failed",
          "trigger": "tick",
          "error": "repository offsite: restic backup failed: exit status 3: ...",
          "repositories": [
            {"name": "nas", "result": "ok", "snapshot_id": "a1b2c3d4"},
            {"name": "offsite", "result": "backup_failed", "error": "..."}
          ]
        }
      ]
    }
  }
}
```

An old file has no `runs`, so `status` shows the last run time and outcome with no detail. An old binary reading a new file ignores the new fields, and drops them the next time it writes; a downgrade loses history and nothing else.

A separate history file was considered. One file means one lock and one atomic write, and the size is small: 20 runs of a few hundred bytes per job.

Repository `result` is one of `ok`, `backup_failed` or `forget_failed`. Errors are flattened to one line and capped with the existing `oneLine`, the same text hooks receive in `RESTOMATIC_ERROR`.

### History of 20 runs per job, a constant
Enough to see a pattern (most of a day for an hourly job, three weeks for a daily one) without making the file large. Not configurable; the central app will keep longer history from reports.

### A cross-process lock around every state update
`Store` takes an exclusive `flock` on `<state-dir>/locks/state.lock` for the read-modify-write, waiting up to five seconds. The store is given the lock directory when it is created, so it doesn't have to know the state directory's layout. It is held for milliseconds. All writers go through one method that takes a function to apply to the loaded state, so the lock can't be forgotten.

This matters more now than before: every `tick` writes the tick time, so writes overlap with long-running jobs finishing far more often than they did.

### "Running" comes from the existing per-job lock
`status` tests the job lock that `fix-job-concurrency` introduced. Because the kernel releases a `flock` when its holder dies, a killed process never leaves a job looking as if it is running.

The start time and trigger are written to the job's `running` field in the state file when the run begins and removed when it ends. `status` believes that field only while the lock is held, so a stale `running` left by a killed process is ignored. A job whose lock is held but which has no `running` field is queued and waiting to start, and is shown as waiting. To keep that reliable, a process that takes a job lock removes any `running` field left behind by an earlier, killed execution.

Storing the start time inside the lock file was considered. On Windows a locked file can't be read through another handle, so the state file is used.

Recording a "started" marker alone, with no lock, was rejected: it can't tell a running job from one whose process was killed.

### The status probe must not create files or block a starting job
- `status` tests a job lock only if the lock file already exists, so it never creates one. A job with no lock file is not running. This keeps `status` safe to run as root against a user's state directory.
- The test takes a shared lock and releases it at once. A job starting in that instant would fail to get its exclusive lock, so the executor retries for a fraction of a second before concluding the job is running.

### Next due time comes from the same boundaries as `Due`
A new `schedule.Next(sched, lastRun)` returns the start of the period after the one containing `lastRun`: the next hour, the next UTC midnight, or the next Sunday at UTC midnight. `status` reports `due` from `schedule.Due` itself and `next_due` from `Next`, so the two can't disagree with `tick`. A job that has never run is due, with no next due time.

Boundaries are in UTC, as they are today. The default output shows them in local time.

### Tick time is recorded when schedules are evaluated
Written after the config has loaded and the owner check has passed, before any job starts. A tick refused for an invalid config did not do its job, so it should not make the scheduler look healthy.

### JSON is the same facts, versioned
```json
{
  "format_version": 1,
  "generated_at": "2026-10-01T19:04:00Z",
  "last_tick": "2026-10-01T19:03:00Z",
  "jobs": [
    {
      "name": "gitea",
      "schedule": "daily",
      "repositories": ["nas", "offsite"],
      "due": false,
      "next_due": "2026-10-02T00:00:00Z",
      "waiting": false,
      "running": null,
      "last_run": { "...": "one run record, as in the state file" }
    }
  ]
}
```
`waiting` is true for a job queued behind a busy repository or slot; `running` is `{"started": ..., "trigger": ...}` once it has begun. At most one of the two is set.

`status <job> --json` has the same shape with one job, plus that job's `runs`. Absent values are `null`, not omitted, so consumers can rely on the keys. `format_version` changes only for an incompatible change; adding keys does not change it.

The JSON is built from a struct that the text output is also rendered from, so the two can't drift apart.

### Default output
```
last tick: 2 minutes ago

JOB        SCHEDULE  LAST RUN        OUTCOME  TOOK   NEXT DUE
documents  daily     6 hours ago     ok       1m12s  in 18 hours
gitea      daily     2 hours ago     FAILED   48s    in 22 hours
  nas: ok (snapshot a1b2c3d4)
  offsite: backup failed: restic backup failed: exit status 3: ...
media      weekly    never           -        -      due now
postgres   hourly    1 hour ago      ok       8s     due since 19:00
  running since 3 minutes ago (started by tick)
```
Outcome labels use the existing colour helpers.

The OUTCOME column always shows the last finished run. A job that is running or waiting says so on a line beneath it. An earlier draft put `running` in the OUTCOME column, but that hid a failed last run for as long as the next one took, which is exactly when someone looks.

The table abbreviates: snapshot IDs are cut to eight characters, as restic's own listings do, and a note longer than 200 characters is clipped with an ellipsis, because restic's errors can run to several hundred. The JSON output carries both in full.

### `status` loads and validates the config
The job list, schedules and repository names come from the config, so an invalid config is an error. Warnings go to standard error as they do for every other command, which keeps `--json` output clean.

## Risks / Trade-offs

- **[Risk] Error text can contain secrets,** such as credentials embedded in a repository URL that restic echoes. → The state file is already owner-only. `status` prints the text, as `tick` does today. The later report change must consider this before sending errors off the host.
- **[Risk] The state lock can't be taken within the wait.** → Treated as a state write failure: a warning, and the job's outcome is unchanged. The job would run again at the next tick, which is the existing consequence of a failed state write.
- **[Trade-off] One more state write per job (the `running` field) and one per tick.** → Each is a small atomic file replace. At a one-minute tick that is one write a minute on an idle host.
- **[Trade-off] `status` can't flag a dead scheduler by itself.** It shows the last tick time and leaves the judgement to the reader or the central app, which knows the expected interval.

## Migration Plan

Nothing to do. Existing state files are read as they are and gain the new fields as jobs run. Rolling back to an earlier binary works; it drops the history the next time it writes state.
