# Status

`rest-o-matic status` shows what has been recorded about every job, without
running anything or touching a repository:

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

- **last tick** is when `tick` last ran. If it's older than your cron
  interval, the scheduler has stopped firing.
- **NEXT DUE** is when `tick` will next start the job. Schedule boundaries
  are in UTC (a daily job is due from 00:00 UTC); they are shown here in
  your local time.
- A failed job lists the result for each repository it tried, or the error
  of whatever else failed (a hook, an interruption).
- A job that has started is shown as `running`; one queued behind a busy
  repository or slot is shown as `waiting` (see [Concurrency](concurrency.md)).

`rest-o-matic status <job>` lists that job's recent runs, newest first. The
last 20 are kept.

`rest-o-matic status --json` prints the same information as a single JSON
document, with full snapshot IDs and untruncated errors, for scripts and
monitoring. Times are UTC, and values that don't apply are `null`.

`status` exits 0 whenever it could report, whatever the health of the jobs,
and only reads: it is safe to run as any user that can read the state
directory.
