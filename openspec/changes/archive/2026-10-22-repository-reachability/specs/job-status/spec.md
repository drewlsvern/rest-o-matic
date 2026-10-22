## MODIFIED Requirements

### Requirement: Failed Runs Show What Failed
For a job whose most recent run failed, `status` SHALL show the recorded error for that run and the result for each repository the run attempted, so that the failing repository can be identified without consulting any other log. An unreachable repository SHALL be shown as unreachable with how long it was checked for, separately from a failed backup. When some of the run's repositories succeeded, the default output SHALL show the outcome as failed together with how many of how many repositories succeeded, such as `FAILED (1 of 2 ok)`; this SHALL NOT change the recorded outcome or the JSON, where the outcome stays failed.

#### Scenario: One repository failed
- **WHEN** the `gitea` job's last run backed up to `nas` successfully and failed backing up to `offsite`
- **THEN** `status` SHALL show `gitea` as `FAILED (1 of 2 ok)`, SHALL show the error recorded for `offsite`, and SHALL show `nas` as successful

#### Scenario: One repository unreachable
- **WHEN** the `config` job's last run succeeded on `ut-g5` and found `s3` unreachable with the default timeout
- **THEN** `status` SHALL show `config` as `FAILED (1 of 2 ok)` and SHALL show `s3` as unreachable, checked for 2 minutes, with its recorded error

#### Scenario: Every repository failed
- **WHEN** a job's last run failed on each of its repositories
- **THEN** `status` SHALL show it as `FAILED`, with no count

#### Scenario: Before hook failed
- **WHEN** a job's last run failed because a `before` hook exited non-zero
- **THEN** `status` SHALL show the job as failed with that hook's error, and SHALL show no repository results

### Requirement: Running Jobs Show Their Progress
While `status` shows a job as running, it SHALL also show the job's recorded progress: its phase and, during a check or a backup, the repository with its position; during a backup, percent done, bytes done and in total, the throughput (bytes done divided by the seconds the backup has run), and the estimated time left when restic gave one. `status <job>` and `status --json` SHALL also show the files being read, when restic last reported, and when its figures last increased.

The default output SHALL also say, in place of the time left:
- "no progress reported for" a duration, when the figures have not increased for more than a minute;
- "no report from restic yet" with how long the backup has run, when a backup has run for more than a minute without any report from restic.

During a check or a backup, when restic has printed a retry message more recently than its last progress report, the default output SHALL add "restic is retrying:" followed by that message, and `status <job>` and `status --json` SHALL also show when it was seen.

All of this SHALL be shown only: `status` SHALL NOT warn about it, change its exit status, or stop anything.

Progress SHALL be shown only while the job's lock is held, by the same rule that decides whether the job is shown as running. A progress record without a running job, such as one left by a killed process, SHALL be ignored. A job that is running but has recorded no progress yet SHALL be shown as running, as before. `status` SHALL NOT remove or change a progress record.

#### Scenario: Backup in progress
- **WHEN** `status` is run while the `config` job is 43% through its backup to `s3`, the first of two repositories, after 5 minutes at 12.4 MiB/s
- **THEN** the line for `config` SHALL show that it is backing up to `s3` (1 of 2), 43% done, with bytes done and in total, `12.4 MiB/s` and the time left

#### Scenario: Paused
- **WHEN** the `config` job's figures have not increased for 12 minutes, while restic still reports
- **THEN** `status` SHALL say "no progress reported for 12 minutes" for `config` and SHALL exit with status zero

#### Scenario: No report yet
- **WHEN** the `config` job's backup to `s3` started 3 minutes ago and restic has sent no progress report
- **THEN** `status` SHALL say "no report from restic yet (3 minutes)" for `config`

#### Scenario: Retrying
- **WHEN** the `config` job is checking `s3` and restic's latest retry message is `dial tcp 192.0.2.1:443: i/o timeout`
- **THEN** `status` SHALL show `config` checking `s3` (1 of 2), followed by "restic is retrying: dial tcp 192.0.2.1:443: i/o timeout"

#### Scenario: In a hook
- **WHEN** `status` is run while the `mssql-dev` job runs its before hooks
- **THEN** it SHALL show `mssql-dev` as running its before hooks

#### Scenario: Left by a killed process
- **WHEN** the process running `config` was killed with SIGKILL, leaving its progress record, and nothing is running `config`
- **THEN** `status` SHALL NOT show `config` as running and SHALL NOT show its progress

#### Scenario: JSON
- **WHEN** `status --json` is run while `config` is backing up
- **THEN** its entry for `config` SHALL carry the phase and when it started, the repository, restic's figures including seconds elapsed, the files being read, the latest retry message and when it was seen, and the times of the last report and last movement, as RFC 3339 timestamps in UTC; a running job with no recorded progress SHALL have no progress in its entry
