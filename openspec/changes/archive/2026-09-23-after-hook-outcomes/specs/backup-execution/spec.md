## MODIFIED Requirements

### Requirement: Job-Level Hook Execution
A job's `before` hooks SHALL run once per job execution, prior to any repository backup. A job's `after` hooks SHALL run once per job execution, after all configured repositories have been attempted, in this fixed order regardless of how they are written in the config: first the `always` commands, then either the `success` commands or the `failure` commands, never both. Hooks SHALL NOT be re-run per repository.

#### Scenario: Hooks run once regardless of repository count
- **WHEN** a job with `before`, `always`, `success` and `failure` hooks lists two repositories and every step succeeds
- **THEN** the before hooks SHALL execute exactly once, followed by a backup attempt against each of the two repositories, followed by the always hooks exactly once, followed by the success hooks exactly once, and the failure hooks SHALL NOT run

#### Scenario: Always runs before outcome hooks whatever the key order
- **WHEN** a job's config lists `failure:` above `always:` in its `after` map, and a repository backup fails
- **THEN** the always hooks SHALL complete before any failure hook starts

### Requirement: Before-Hook Failure Aborts Backup
If any `before` hook exits with a non-zero status, the job SHALL NOT proceed to back up any repository, and the remaining `before` commands SHALL NOT run. The job's `always` hooks and then its `failure` hooks SHALL still run before the job is reported as failed.

#### Scenario: Failed before hook skips all repository backups
- **WHEN** a job's before hook exits non-zero
- **THEN** no `restic backup` invocation SHALL be attempted for any of the job's repositories, the always hooks and then the failure hooks SHALL run, the success hooks SHALL NOT run, and the job's outcome SHALL be recorded as failed

## ADDED Requirements

### Requirement: Job Outcome Selects Success or Failure Hooks
A job SHALL be considered successful only if its `before` hooks succeeded, every configured repository's backup and retention enforcement succeeded, and every `always` command succeeded. Otherwise it SHALL be considered failed. This includes a repository deferred because another execution held its lock, and an execution interrupted by a signal. The `success` hooks SHALL run only for a successful job, and the `failure` hooks only for a failed one. A failing `success` or `failure` command SHALL be reported but SHALL NOT change the job's outcome.

#### Scenario: Failed always command fails the job
- **WHEN** every repository backup succeeds but an `always` command exits non-zero
- **THEN** the job SHALL be reported and recorded as failed, and the failure hooks SHALL run instead of the success hooks

#### Scenario: One failed repository fails the job
- **WHEN** a job has two repositories and only one of the backups fails
- **THEN** the failure hooks SHALL run and the success hooks SHALL NOT

#### Scenario: Failing outcome hook does not change the outcome
- **WHEN** a job fully succeeds and one of its success commands exits non-zero
- **THEN** the job SHALL still be recorded as successful, and the command's failure SHALL be reported in the job's output

### Requirement: Post-Backup Hooks Run Every Command
Within each of the `always`, `success` and `failure` lists, every command SHALL be run even if an earlier command in the same list fails, and every failure SHALL be reported. (`before` commands keep stopping at the first failure.)

#### Scenario: Later always command runs after an earlier one fails
- **WHEN** a job's `always` list is `[start-app.sh, start-db.sh]` and `start-app.sh` exits non-zero
- **THEN** `start-db.sh` SHALL still run, and both the failure of `start-app.sh` and the job's resulting failure SHALL be reported

### Requirement: Hook Environment
Every hook command SHALL run with the calling environment plus `RESTOMATIC_JOB` set to the job's name. `success` and `failure` commands SHALL additionally receive:
- `RESTOMATIC_OUTCOME`: `success` or `failure`;
- `RESTOMATIC_FAILED_REPOS`: a comma-separated list of repositories whose backup or retention enforcement failed or was deferred, empty if none; and
- `RESTOMATIC_ERROR`: a one-line description of the first failure, empty on success.

#### Scenario: Failure hook sees the outcome and failed repository
- **WHEN** a job backing up to repositories `nas` and `s3` fails only on `s3`
- **THEN** its failure commands SHALL see `RESTOMATIC_JOB` set to the job's name, `RESTOMATIC_OUTCOME=failure`, `RESTOMATIC_FAILED_REPOS=s3`, and a non-empty `RESTOMATIC_ERROR`

#### Scenario: Before hook sees the job name
- **WHEN** a job's before hook runs
- **THEN** it SHALL see `RESTOMATIC_JOB` set to the job's name

### Requirement: Interruption Runs Cleanup Hooks
When rest-o-matic running `run` or `tick` receives SIGTERM or SIGINT, it SHALL:
1. stop any in-flight restic or hook command belonging to a running job;
2. start no further repository backups, and no job that has not yet started;
3. run each started job's `always` hooks and then its `failure` hooks, with `RESTOMATIC_ERROR` indicating the interruption;
4. record each started job as failed; and
5. exit with a non-zero status.

Cleanup SHALL be bounded by a time limit, after which any cleanup command still running SHALL be stopped. If a second SIGTERM or SIGINT arrives during cleanup, rest-o-matic SHALL stop the cleanup and exit immediately.

#### Scenario: Container restarted after interruption mid-backup
- **WHEN** rest-o-matic receives SIGTERM while a job whose `before` stopped a container is backing up, and whose `always` starts it again
- **THEN** the backup SHALL be stopped, the always hooks SHALL run (restarting the container), the failure hooks SHALL run, the job SHALL be recorded as failed, and the process SHALL exit non-zero

#### Scenario: Queued jobs do not start after interruption
- **WHEN** `tick` receives SIGINT while one job is running and another due job is waiting for a concurrency slot
- **THEN** the waiting job SHALL NOT start and SHALL NOT run any hooks

#### Scenario: Hung cleanup is bounded
- **WHEN** an `always` command does not finish within the cleanup time limit after an interruption
- **THEN** that command SHALL be stopped and rest-o-matic SHALL exit

#### Scenario: Second signal skips cleanup
- **WHEN** a second SIGINT arrives while cleanup hooks are running
- **THEN** rest-o-matic SHALL stop the cleanup commands and exit immediately
