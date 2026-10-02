# job-status Specification

## Purpose

Lets someone on a host see with one command whether each backup job is working, when it last ran and when it will next run, and gives scripts and other tools the same information as JSON.

## Requirements

### Requirement: Status Lists Every Configured Job
The `status` command SHALL list every job in the config, in name order. For each job it SHALL show the job's schedule, when it last ran, the outcome of that run, how long that run took, and when it is next due. A job with no recorded run SHALL be shown as never run.

#### Scenario: Successful job
- **WHEN** the `documents` job last ran successfully, starting at 02:00:05 and finishing at 02:01:17
- **THEN** `status` SHALL show `documents` with a successful outcome, that run's time, and a duration of 1 minute 12 seconds

#### Scenario: Job that has never run
- **WHEN** the `media` job is in the config and has no recorded run
- **THEN** `status` SHALL show `media` as never run and as due

#### Scenario: Jobs are in name order
- **WHEN** the config defines jobs `postgres`, `documents` and `gitea`
- **THEN** `status` SHALL list them as `documents`, `gitea`, `postgres`

### Requirement: Failed Runs Show What Failed
For a job whose most recent run failed, `status` SHALL show the recorded error for that run and the result for each repository the run attempted, so that the failing repository can be identified without consulting any other log.

#### Scenario: One repository failed
- **WHEN** the `gitea` job's last run backed up to `nas` successfully and failed backing up to `offsite`
- **THEN** `status` SHALL show `gitea` as failed, SHALL show the error recorded for `offsite`, and SHALL show `nas` as successful

#### Scenario: Before hook failed
- **WHEN** a job's last run failed because a `before` hook exited non-zero
- **THEN** `status` SHALL show the job as failed with that hook's error, and SHALL show no repository results

### Requirement: Next Due Time Matches Tick
The next due time shown for a job SHALL be the time from which `tick` would start that job. A job that `tick` would start now SHALL be shown as due.

#### Scenario: Hourly job not yet due
- **WHEN** an hourly job last ran at 14:10 and `status` runs at 14:30
- **THEN** the job SHALL be shown as next due at 15:00 and SHALL NOT be shown as due

#### Scenario: Boundary has passed
- **WHEN** an hourly job last ran at 14:10 and `status` runs at 15:20
- **THEN** the job SHALL be shown as due since 15:00

### Requirement: Running Jobs Are Shown
`status` SHALL show a job as running, with the time it started, while an execution of that job is in progress in any process. A job that is queued and waiting to start, because a repository it needs or every concurrency slot is in use, SHALL be shown as waiting and not as running. A job SHALL NOT be shown as running or waiting once that execution has ended, including when its process was killed without the chance to clean up.

#### Scenario: Job waiting for a repository
- **WHEN** a tick is waiting to start the `documents` job because another job is using its repository
- **THEN** `status` SHALL show `documents` as waiting

#### Scenario: Long backup in progress
- **WHEN** `tick` started the `gitea` job ten minutes ago and it has not finished
- **THEN** `status` SHALL show `gitea` as running since that start time

#### Scenario: Killed process is not shown as running
- **WHEN** the process running the `gitea` job was killed with SIGKILL and no other execution of `gitea` is in progress
- **THEN** `status` SHALL NOT show `gitea` as running

### Requirement: Last Tick Is Shown
`status` SHALL show the time of the most recent recorded tick, or that no tick has been recorded.

#### Scenario: Scheduler is firing
- **WHEN** the most recent tick was recorded two minutes ago
- **THEN** `status` SHALL show that time as the last tick

#### Scenario: No tick recorded
- **WHEN** jobs have only ever been started with `run`
- **THEN** `status` SHALL show that no tick has been recorded

### Requirement: Single-Job Run History
`status <job>` SHALL show the recorded runs of that job, newest first, each with its start time, duration, outcome, how it was started, and its error if it failed. Naming a job that is not in the config SHALL be an error with a non-zero exit status.

#### Scenario: Recent runs listed
- **WHEN** the `postgres` job has five recorded runs and `status postgres` is run
- **THEN** all five SHALL be listed, newest first

#### Scenario: Unknown job
- **WHEN** `status nosuchjob` is run and the config has no job of that name
- **THEN** the command SHALL fail with a non-zero exit status and name the job

### Requirement: JSON Output
With `--json`, `status` SHALL write exactly one JSON document to standard output and nothing else, containing the same information as the default output. The document SHALL carry a format version number. Times SHALL be RFC 3339 timestamps in UTC. Config warnings and any other messages SHALL go to standard error.

#### Scenario: Output is a single JSON document
- **WHEN** `status --json` is run against a config that produces a validation warning
- **THEN** standard output SHALL parse as one JSON document, and the warning SHALL appear on standard error only

#### Scenario: JSON carries the same facts
- **WHEN** `status --json` is run and the `gitea` job's last run failed on repository `offsite`
- **THEN** the document's entry for `gitea` SHALL contain the failed outcome, the error, and a result for each repository attempted

#### Scenario: Single job as JSON
- **WHEN** `status postgres --json` is run
- **THEN** the document SHALL contain only the `postgres` job, including its recorded runs

### Requirement: Status Is Read-Only
`status` SHALL NOT create or modify any file in the state directory, SHALL NOT invoke restic, and SHALL NOT run any hook. It SHALL NOT be subject to the state directory owner check. A state directory that does not exist SHALL be treated as no recorded runs.

`status` SHALL exit with status zero whenever it was able to report, whatever the health of the jobs. It SHALL exit non-zero only when it could not report, such as an invalid config or an unreadable state file.

#### Scenario: Missing state directory
- **WHEN** `status` is run and the state directory does not exist
- **THEN** every job SHALL be shown as never run, and the state directory SHALL NOT be created

#### Scenario: Failed jobs do not fail the command
- **WHEN** `status` is run and one job's last run failed
- **THEN** the command SHALL exit with status zero

#### Scenario: Run as another user
- **WHEN** root runs `status` against a state directory owned by the user `me`
- **THEN** it SHALL report normally and SHALL leave no root-owned file in the state directory

### Requirement: Failing Since Is Shown
For a job whose most recent run failed, `status` SHALL show the time of the first failed run of its current run of failures, in both the default output and the JSON output. For a job that is not failing, the JSON value SHALL be null. This SHALL be reported whether or not any notification commands are configured.

#### Scenario: Job failing for several runs
- **WHEN** a job has failed on every run since Monday at 02:00
- **THEN** `status` SHALL show that it has been failing since Monday at 02:00

#### Scenario: Healthy job
- **WHEN** a job's most recent run succeeded
- **THEN** `status --json` SHALL report a null failing-since time for it
