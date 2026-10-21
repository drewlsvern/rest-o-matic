## ADDED Requirements

### Requirement: Running Jobs Show Their Progress
While `status` shows a job as running, it SHALL also show the job's recorded progress: its phase and, during a backup, the repository with its position, percent done, bytes done and in total, and the estimated time left when restic gave one. `status <job>` and `status --json` SHALL also show the files being read, when restic last reported, and when its figures last increased. When the figures have not increased for more than a minute, the default output SHALL say "no progress reported for" that long, in place of the time left. That SHALL be shown only: `status` SHALL NOT warn about it, change its exit status, or stop anything.

Progress SHALL be shown only while the job's lock is held, by the same rule that decides whether the job is shown as running. A progress record without a running job, such as one left by a killed process, SHALL be ignored. A job that is running but has recorded no progress yet SHALL be shown as running, as before. `status` SHALL NOT remove or change a progress record.

#### Scenario: Backup in progress
- **WHEN** `status` is run while the `config` job is 43% through its backup to `s3`, the first of two repositories
- **THEN** the line for `config` SHALL show that it is backing up to `s3` (1 of 2), 43% done, with bytes done and in total and the time left

#### Scenario: Paused
- **WHEN** the `config` job's figures have not increased for 12 minutes, while restic still reports
- **THEN** `status` SHALL say "no progress reported for 12 minutes" for `config` and SHALL exit with status zero

#### Scenario: In a hook
- **WHEN** `status` is run while the `mssql-dev` job runs its before hooks
- **THEN** it SHALL show `mssql-dev` as running its before hooks

#### Scenario: Left by a killed process
- **WHEN** the process running `config` was killed with SIGKILL, leaving its progress record, and nothing is running `config`
- **THEN** `status` SHALL NOT show `config` as running and SHALL NOT show its progress

#### Scenario: JSON
- **WHEN** `status --json` is run while `config` is backing up
- **THEN** its entry for `config` SHALL carry the phase, the repository, restic's figures, the files being read, and both times, as RFC 3339 timestamps in UTC; a running job with no recorded progress SHALL have no progress in its entry
