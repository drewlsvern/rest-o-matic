## Context

- A job's due-ness comes from its recorded last run, which is written only when the job finishes ([exec.go](../../../cmd/rest-o-matic/exec.go)). While a job runs, every later `tick` still sees it as due.
- The cross-process guards are a per-repository lock and a set of concurrency slots, both non-blocking `flock` files ([lock.go](../../../internal/lock/lock.go)). There is no per-job guard.
- The slot is taken in `executeWithSlot`. If none is free the job is reported as failed with "no concurrency slot available; deferring".
- The repository lock is taken inside `runRepo`, once per repository, **after** the `before` hooks have run ([job.go](../../../internal/execution/job.go)). If it is held, that repository is marked `Deferred`, which makes the job fail.
- A failed run is recorded like any other, so the job is not due again until its next schedule boundary.

### Reproduction 1: a running job is started again

A job whose `before` hook sleeps four seconds; a second `tick` started 1.5 seconds after the first. The hook log and the second tick's output:

```
01.06 stop-container   (tick 1, before)
02.56 stop-container   (tick 2, before)
06.58 start-container  (tick 1, always)
06.59 start-container  (tick 2, always)

job app: FAILED
  repository r: deferred (locked by another execution)
tick: 1 job(s) due, 0 succeeded, 1 failed
```

With a long backup, tick 2's `always` hook starts the container while tick 1 is still reading its data.

### Reproduction 2: two jobs, one repository, same schedule

Two daily jobs backing up to `nas`, one `tick`:

```
job documents: FAILED
  repository nas: deferred (locked by another execution)
job postgres: OK
  repository nas: ok
tick: 2 job(s) due, 1 succeeded, 1 failed
```

A second `tick` found nothing due, and `nas` held only the `postgres` snapshot. `documents` did not run again that day.

### Noticed and left alone

- The `scheduling` spec says due-ness is relative to a job's last *successful* run; the code uses the last run of any outcome, so a job that genuinely fails is not retried until its next boundary.
- Due-ness uses the finish time, so a daily job that starts at 23:58 and ends at 00:03 counts as the second day's run.

## Goals / Non-Goals

**Goals:**
- A job never has two executions at once.
- A job that can't start yet waits and then runs, in the same invocation.
- Nothing of a job runs until it can run completely: a container is never stopped for a backup that then can't proceed.
- No cleanup is ever needed after a crash or a kill.

**Non-Goals:**
- Changing `exec`. It keeps refusing at once with exit code 20 when a repository is in use; a person at a terminal should be told, not left waiting.
- A time limit on waiting or on a job. Hook and backup timeouts are a separate, known gap.
- Strict first-come-first-served order between separate processes (see Risks).
- Showing which jobs are running or waiting. `job-status-reporting` adds that.
- The two items under "Noticed and left alone".

## Decisions

### Acquire everything up front, in a fixed order
Before `RunJob` is called, `executeWithSlot` obtains, in this order:

1. **The job lock**, without waiting. If it is held, the job is already running or waiting: stop here.
2. **Every repository lock the job needs**, waiting for each, in sorted name order with duplicates removed.
3. **A concurrency slot**, waiting.

Then the job runs, and everything is released after its state has been written.

`RunJob` and `runRepo` no longer lock anything. `RepoOutcome.Deferred`, and the code that reports it, are removed.

Waiting before the hooks, not after, is the point of the design: a job waiting behind a two-hour backup does nothing at all until it can proceed, so its application stays up.

Leaving a blocked job due and retrying on the next tick was considered. It needs no waiting, but its hooks would run and be undone on every tick until the repository was free, and a job with two repositories would back up the free one repeatedly.

### Why this order can't deadlock
- Repository locks are always taken in sorted order, so two jobs that share repositories can never each hold one the other needs.
- A slot is the last thing taken, so whoever holds a slot is running and needs nothing more.
- The job lock is never waited for.

Removing duplicate repository names matters: a job that listed the same repository twice would otherwise wait for itself.

### The job lock is what stops ticks piling up
With a one-minute cron entry and a job waiting two hours, every later tick would otherwise start waiting for the same job. Because the first waiter holds the job lock from before it starts waiting, later ticks see "already running" and exit. Only one process ever waits for a given job.

The job lock is a non-blocking exclusive `flock` on `<state-dir>/locks/job-<name>.lock`, like the existing locks. The kernel releases it when its holder exits for any reason.

### `tick` re-checks due-ness after taking the job lock
A tick works from the list of due jobs it computed when it started. If it then has to queue a job behind its own `max_concurrent` limit, a later tick can run that job in the meantime. So once `tick` holds a job's lock, it reloads the state and confirms the job is still due; if not, it releases the lock and moves on without reporting anything. `run` does not check, since it runs a job regardless of schedule.

### Held until state is written
The locks are released only after `executeAndRecord` has written the run's state. Releasing the job lock earlier would leave a moment in which the job is neither locked nor recorded, and a tick landing in it would start the job again.

### Waiting is a polled, cancellable retry
`flock` has no timed or cancellable wait, so waiting retries the non-blocking attempt about once a second until it succeeds or the work context is cancelled. For slots, each retry tries every slot file.

When a job starts waiting, one line is printed (`job <name>: waiting for repository <repo>` or `waiting for a free slot`) so a cron log explains a long-running tick.

### Three results, not two
`executeWithSlot` returns a job result and a `report` flag today. The flag becomes a result kind:

- **ran**: has a real outcome, printed and counted as today.
- **not started**: interrupted before it began, including while waiting. Counted as today.
- **already running**: `tick` prints `job <name>: already running, skipped`, leaves it out of the succeeded and failed counts, adds `N already running` to the summary, and does not exit non-zero for it. `run` returns `job %q is already running`.

Nothing is recorded in the state file for the last two.

### Within one tick
`dispatch` already limits goroutines to `max_concurrent`. A goroutine that is waiting for a repository holds one of those in-process places but not a cross-process slot. Jobs are dispatched in sorted order, as today.

## Risks / Trade-offs

- **[BREAKING] `tick` and `run` can now run for a long time while waiting**, where they used to fail at once. → Intended. Each waiting job prints why. Later ticks skip a job that is already waiting, so processes don't accumulate beyond one per waiting job.
- **[Trade-off] A job holds all its repositories for its whole run.** Before, it held each only while backing up to it, so two jobs with the same two repositories could sometimes overlap. → Accepted: it is what makes "nothing runs until everything is free" possible, and jobs sharing repositories now always complete.
- **[Risk] The order in which waiting jobs start is not guaranteed.** When a repository is released, whichever waiter retries first gets it, whether the waiters are in one tick or several. A tick launches its jobs in sorted order, so the first of them usually goes first, but nothing enforces it. → Every waiting job still runs. The spec's "Queue order follows due order" scenario is not fully met; this was discussed on 2026-10-01 and deliberately left for later. A per-repository queue inside one tick would cover the common case.
- **[Risk] A hung job blocks everything waiting behind it.** → True today through the repository lock, though today the others fail loudly. The waiting line in the log, and later `status`, show what is being waited for.
- **[Risk] Waiters hold in-process places.** With `max_concurrent: 2`, two jobs waiting on a busy repository delay a third, unrelated due job in the same tick until one of them starts. → Accepted for now; the next tick picks the third job up, since it is a different job and is not locked, and the due-ness re-check stops the first tick repeating it.
- **[Trade-off] One more lock file per job** in `locks/`, never removed, like the others.

## Migration Plan

Nothing to do. Lock files are created on first use. Anyone who relied on a `failure` hook firing when a job was deferred will no longer get that alert, because the job now runs.
