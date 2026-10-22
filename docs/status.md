# Status

`rest-o-matic status` shows what has been recorded about every job, without
running anything or touching a repository:

```
last tick: 2 minutes ago
central app: https://backups.example.com, last check-in 2 minutes ago

JOB        SCHEDULE  LAST RUN        OUTCOME  TOOK   NEXT DUE
documents  daily     6 hours ago     ok       1m12s  in 18 hours
gitea      daily     2 hours ago     FAILED   48s    in 22 hours
  nas: ok (snapshot a1b2c3d4)
  offsite: backup failed: restic backup failed: exit status 12: wrong password or no key found
media      weekly    never           -        -      due now
postgres   hourly    1 hour ago      ok       8s     due since 19:00
  running since 3 minutes ago (started by tick)
```

- **last tick** is when `tick` last ran. If it's older than your cron
  interval, the scheduler has stopped firing.
- **central app** shows whether the host reports to the
  [central app](central-app.md), when it last did, and why the latest
  attempt failed if it did. It also says when the config is not being
  backed up there, and why.
- **NEXT DUE** is when `tick` will next start the job. Schedule boundaries
  are in UTC (a daily job is due from 00:00 UTC); they are shown here in
  your local time.
- A failed job lists the result for each repository it tried, or the error
  of whatever else failed (a hook, an interruption). When some of its
  repositories still worked, OUTCOME says how many: `FAILED (1 of 2 ok)`.
  A repository that couldn't be opened in time shows as
  `unreachable (checked for 2m)`, with restic's reason (see
  [Unreachable repositories](configuration.md#unreachable-repositories)).
- A job that has started is shown as `running`, with where it has got to
  (see [A running job's progress](#a-running-jobs-progress)); one queued
  behind a busy repository or slot is shown as `waiting` (see
  [Concurrency](concurrency.md)).

`rest-o-matic status <job>` lists that job's recent runs, newest first. The
last 20 are kept.

## A running job's progress

While a job runs, `status` shows what it is doing, whether `tick` or `run`
started it:

```
config     daily     1 day ago       ok       2h4m   due since 00:00
  running since 1 hour ago (started by tick): backing up to s3 (1 of 2), 43%, 3.4 GiB of 7.9 GiB, 12.4 MiB/s, about 1 hour left
mssql-dev  weekly    6 days ago      ok       3m10s  due since 00:00
  running since 2 minutes ago (started by tick): before hooks
```

The phases are `before hooks`, `checking <repository> (i of n)` (the
[connection check](configuration.md#unreachable-repositories), usually over
in under a second), `backing up to <repository> (i of n)`,
`applying retention on <repository> (i of n)` and `after hooks`. During a
backup the figures are restic's own and at most a few seconds old. The total
grows while restic is still counting files, so the percentage can drop back
early in a run.

The rate (`12.4 MiB/s`) is how much restic has read divided by how long the
backup has run. On a first backup, or one that is waiting on its uploads,
that is about the upload speed. On a backup of mostly unchanged files it is
much higher, because restic skips unchanged files quickly.

While restic is retrying something, typically a connection, its latest
reason is shown until it reports progress again:

```
  running since 1 minute ago (started by run): checking s3 (1 of 2), restic is retrying: Stat: Get "https://s3.example.com/b/?location=": dial tcp 192.0.2.10:443: i/o timeout
```

A backup that restic hasn't reported on at all for over a minute says so,
since restic sends nothing while it opens a repository or loads its index:

```
  running since 4 minutes ago (started by tick): backing up to s3 (1 of 2), no report from restic yet (3 minutes)
```

When restic's figures haven't moved for over a minute, the time left is
replaced by how long they have stood still:

```
  running since 1 hour ago (started by tick): backing up to s3 (1 of 2), 43%, 3.4 GiB of 7.9 GiB, no progress reported for 12 minutes
```

That isn't necessarily a fault. On a slow upload restic stops reading while
it sends what it has, and over a slow link that can take minutes.
rest-o-matic only reports it: nothing warns, times out or stops the job.
`status <job>` tells the two apart. It shows the files being read, when
restic last reported, and when its figures last moved:

```
running since 1 hour ago (started by tick): backing up to s3 (1 of 2), 43%, 3.4 GiB of 7.9 GiB, no progress reported for 12 minutes
  reading /home/me/.config/Code/User/workspaceStorage/state.vscdb
  restic last reported just now
  figures last moved 12 minutes ago
```

If restic still reports but the figures stand still, it's waiting on the
upload. If restic has stopped reporting too, something is wrong.

Progress is shown only while the job's lock is held, so a job whose process
was killed is never shown with the progress it had.

## Snapshots

`status <job>` also lists the snapshots that job has in each of its
repositories:

```
Snapshots in nas, as of 2 hours ago: 3
ID        TIME              SIZE     CHANGED
49bcad91  2026-10-02 02:00  1.2 GiB  12 new, 3 changed, +45.0 MiB
0a3ca2ce  2026-10-01 02:00  1.2 GiB  0 new, 1 changed, +3.1 KiB
e974615e  2026-09-30 02:00  1.1 GiB  4212 new, 0 changed, +1.0 GiB

Snapshots in offsite: not listed yet (a list is taken each time the job runs)
```

The list is a record, not a live view. It is taken each time the job
finishes a run against that repository, from what restic reports as it
enforces retention, so it costs no extra call to the repository and is
shown without contacting it. The "as of" time says how old it is.

- A run that fails for a repository leaves that repository's list as it
  was.
- Snapshots added or removed outside rest-o-matic show up the next time the
  job runs. For the repository's own answer right now, use
  `rest-o-matic exec <repository> -- snapshots --tag <job>`.
- CHANGED is what that snapshot changed compared with the one before it:
  files that were new, files that had changed, and how much it added to
  the repository. It is a quick way to check that a backup picked up a
  change, without listing the snapshot's contents.
- SIZE and CHANGED are shown as `-` with restic older than 0.17, which
  doesn't report them.

## JSON

`rest-o-matic status --json` prints the same information as a single JSON
document, with full snapshot IDs and untruncated errors, for scripts and
monitoring. Times are UTC, and values that don't apply are `null`.

A running job's `running` entry has a `progress` object once the job has
recorded any: `phase` (`before_hooks`, `check`, `backup`, `forget` or
`after_hooks`) and `phase_started`, `repository`, `repository_index` and
`repositories`, restic's `percent_done`, `bytes_done`, `total_bytes`,
`files_done`, `total_files`, `seconds_elapsed`, `seconds_remaining` and
`current_files`, the times `updated`, `last_report` and `last_moved`, and
restic's latest retry message, `last_retry`, seen at `last_retry_at`. It
isn't sent to the central app.

A repository result is `ok`, `unreachable`, `backup_failed` or
`forget_failed`; an `unreachable` one also has `check_timeout`, how long it
was tried for (`2m`).

Every job carries a `snapshot_lists` entry for each of its repositories,
with when the list was taken, how many snapshots it holds and the newest
one's time. `status <job> --json` adds the snapshots themselves.

`checkin` describes the link to the central app: `enrolled`, and, when
enrolled for this config, `url`, `host_id`, `host_name`, `last_attempt`,
`last_success`, `last_error` (null when the latest attempt succeeded) and
`config_withheld` (the fields keeping the config from being sent, or null).

`status` exits 0 whenever it could report, whatever the health of the jobs,
and only reads: it is safe to run as any user that can read the state
directory.
