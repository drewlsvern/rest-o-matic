## Context

- `runRepo` runs `restic backup` and then `restic forget` for one repository ([job.go](../../../internal/execution/job.go)). `Backup` reads restic's standard output line by line (from job-progress), but collects standard error into a buffer that is only read after restic exits, by `resticFailure` and `unreadableHint`.
- What restic does when it can't reach a repository, measured on 2026-10-21 against an S3 endpoint that never answers (192.0.2.1):
  - **restic 0.19.1:** retries with growing, randomised pauses for 824 seconds, then exits 1 with `Fatal: unable to open config file: Stat: … dial tcp …: i/o timeout`. Each attempt waits about 30 s for the TCP connect to time out. Each retry prints a plain line on standard error, with or without `--json`: `Stat(<config/>) returned error, retrying after 1m13.7s: Stat: Get "https://…/?location=": dial tcp 192.0.2.1:443: i/o timeout`.
  - **restic 0.16.4** (Ubuntu 24.04, what CI uses): doesn't retry opening the repository. It exits after the first connect timeout with the same `Fatal:` line, and prints no retry lines.
  - **`restic cat config --no-lock --json`** against a healthy local repository takes about 0.5 s. It fails at once with exit 12 for a wrong password and exit 10 for a missing repository.
- Repository results are strings in the state file (`ok`, `backup_failed`, `forget_failed`, [state.go](../../../internal/state/state.go)). The check-in contract describes them in prose, with no fixed list, and says each side ignores what it doesn't know.
- `progress_interval` already has a duration type (`config.Interval`) with the `Ns`/`Nm`/`Nh`/`0` syntax and a 10 s minimum. Its error messages name `progress_interval`.
- The user's `ut-g5` repository is `backend: local`, but its path is a CIFS share mounted on demand by autofs. rest-o-matic doesn't look at filesystem types.

## Goals / Non-Goals

**Goals:**
- An unreachable repository costs a fixed, chosen time (2 minutes by default) instead of restic's own retry schedule, and is reported as unreachable.
- While restic is struggling to connect, `status` and the progress output say so in restic's own words.
- A slow or silent backup is visible at a glance: throughput, and "no report from restic yet".

**Non-Goals:**
- Retrying a failed job before its next scheduled run. That comes after this change.
- Detecting network filesystems. Every repository is checked instead.
- A limit on how long a backup may run once it has started. A slow backup that is making progress must not be cut off, and restic's own `--stuck-request-timeout` already retries requests that hang.
- Changing `exec`. It stays a passthrough, so you can watch it and stop it yourself.

## Decisions

### The check: `restic cat config`

The check is `restic -r <url> cat config --no-lock --json`, run through the job's read mode with the repository's credentials, under a context with the `connect_timeout` deadline. It opens the repository the same way `backup` does (backend, TLS, credentials, key), so if it succeeds the backup can connect. It reads one small file and takes no lock.

Considered:
- **A TCP or HTTP probe of the endpoint from Go.** That would mean re-implementing every backend's URL parsing, proxies and TLS. It would say nothing about credentials, and would miss local and SFTP paths.
- **`restic snapshots --latest 1`.** It lists the snapshot files, which costs more on a large repository.
- **Putting the deadline on `restic backup` itself until its first progress report.** restic legitimately reports nothing while it loads the index of a large repository, so this would stop healthy backups.

Outcomes:

| Check ends | Result | Backup |
|---|---|---|
| exit 0 | — | runs as before |
| deadline reached | `unreachable`; the error is the last retry message, or "could not be opened within 2m" | skipped, and so is forget |
| non-zero exit within the deadline | `backup_failed`, with restic's error reported as today (`resticFailure`, plus the unreadable-files hint if it applies) | skipped |

With restic 0.16, an endpoint that doesn't answer makes the check fail after the first ~30 s connect timeout, with a non-zero exit, so it is recorded as `backup_failed` with restic's `i/o timeout` message. That's the same as today's behaviour, only reached sooner. `unreachable` is only for the case where rest-o-matic stopped waiting. Telling network errors apart by restic's wording was considered and rejected: it changes between versions, as job-progress found with restic's error output.

When the deadline is reached, restic is stopped the way an interrupted backup is (SIGINT, then the existing kill delay), through the read-mode wrapper as `Backup` does. The check runs inside the `backup` repository phase's progress, as its own `check` phase, so `status` shows "checking s3 (1 of 2)".

### Reading restic's standard error as it arrives

The `backupOutput` line splitter is generalised into one that calls a function per line. Standard output keeps feeding status and summary lines. Standard error now gets one too, which:
- appends the line to a buffer, so `resticFailure` and `unreadableHint` read the same text as today;
- picks out retry lines with `^(.+?) returned error, retrying after [0-9.]+[a-zµ]+(?:[0-9.]+[a-zµ]+)*: (.+)$`, keeps the second group as the message, and passes it to the progress callback as a `Retry` event.

This applies to `Check` and `Backup`. `Forget` is unchanged: it reads its standard output as JSON at the end, and its retries are rare.

The `Progress` interface gains `Retry(job, message string)`. The recorder writes it immediately (as it does a phase change), with `last_retry` and `last_retry_at`. The printer shows it on the terminal line, and in plain mode writes one line per new message. A message identical to the previous one within a phase is still written in plain mode, because the time of each line is the useful part.

### "restic is retrying" display rule

Shown when `last_retry_at` is later than `last_report`, or there is no report at all. A backup that recovers and starts reporting progress therefore stops showing the retry without anything having to clear it.

### "no report from restic yet"

The progress record gains `phase_started`. During a backup with no `last_report`, after more than a minute since `phase_started`, the default output says `no report from restic yet (3 minutes)` in place of the time left. During a check, it isn't shown: a check normally takes under a second, and its timeout is short.

### Throughput

`bytes_done / seconds_elapsed`, both from restic's own report, shown as `12.4 MiB/s` once `seconds_elapsed` is at least 5 (earlier figures are noise). It's restic's processing rate, not purely upload. For a first backup, or one that is waiting on its uploads, the two are about the same. For a backup of mostly unchanged files it's much higher than the upload rate, because unchanged files are skipped quickly. The docs say so. A rate over the last few minutes was considered: it reacts faster to a slowdown, but needs more state in the recorder, and the pause display already covers "stopped moving".

### `FAILED (1 of 2 ok)`

Computed in the `status` text output from the run's repository results. Shown when at least one repository is `ok` and the outcome is failed. It isn't added to `run`'s or `tick`'s result lines, because their standard output stays as it is for scripts. JSON is unchanged.

### `connect_timeout`

`Repository.ConnectTimeout *Interval` and `Config.ConnectTimeout *Interval`. Resolution: the repository's, else the top level's, else 2 m. `Interval`'s parse errors take the key's name instead of always saying `progress_interval`. The same 10 s minimum applies: a shorter limit would fail a healthy repository on a slow TLS handshake. The schema gains both keys.

### `unreachable` in results and the environment

- `state.ResultUnreachable = "unreachable"`.
- `RepoOutcome` gains `Unreachable bool` (with `BackupErr` holding the message), so `Success()` and `FailureSummary()` treat it as a failure. `FailureSummary` reads `repository s3: unreachable (checked for 2m): <message>`.
- `outcomeEnvVars` adds `RESTOMATIC_UNREACHABLE_REPOS`, and keeps `RESTOMATIC_FAILED_REPOS` listing every failed repository, so existing notification scripts still see s3 as failed.
- `status` shows `s3: unreachable (checked for 2m): <error>`. The duration shown is the timeout in effect for that run, which is recorded in the run record as `check_timeout` beside the result, so a later config change doesn't rewrite history.

## Risks / Trade-offs

- [An extra restic process and repository open per repository per run] → About 0.5 s locally. For a remote repository it's one connection and a few small reads. `connect_timeout: 0` turns it off for a repository where that matters.
- [A repository that is merely slow to open, such as a large index over a slow link, is called unreachable at 2 minutes] → The check only reads `config` and the key file, not the index, so a slow index doesn't count against it. Raise `connect_timeout` for a repository on a link where even that is slow.
- [The retry regex depends on restic's wording] → It is only used for display: a line that doesn't match is simply not shown as a retry. It's tested against the 0.19.1 wording, and with 0.16 there are no retry lines anyway.
- [The endpoint comes back between the check and the backup, or goes away between them] → The check is a snapshot in time. If it goes away after a successful check, the backup behaves as it does today: restic's own retries, then `backup_failed`.
- [The UI repo doesn't know `unreachable`] → It is an added value, which the contract says to tolerate. The handoff note tells the UI session.

## Migration Plan

No migration. Existing configs get the check with the 2 m default. An older binary still loads a config that sets `connect_timeout`: it gives an unknown-key warning and ignores the key, so rolling back is safe. Old run records without `check_timeout` show `unreachable` without a duration. They can't hold `unreachable` anyway, since it's new.
