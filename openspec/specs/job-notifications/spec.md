# job-notifications Specification

## Purpose

Lets one set of notification commands cover every job, so that a failing backup is reported without each job needing its own hook, a job that keeps failing does not flood its owner with alerts, and its recovery is reported too.

## Requirements

### Requirement: Notifications Apply to Every Job
The commands in the top-level `notify` block SHALL apply to every job in the config, whether the job is started by `tick` or by `run`. A job SHALL NOT need any configuration of its own for them to apply, and there SHALL be no way for a job to opt out. A job's own hooks SHALL be unaffected and SHALL still run on every run.

Notification commands SHALL run only for a job that actually ran. A job that was skipped because it is already running, or that was interrupted before it started, SHALL NOT produce any notification.

#### Scenario: Failure notification with no job configuration
- **WHEN** the config has `notify: {failure: [alert.sh]}`, the `gitea` job declares no hooks, and `gitea` fails
- **THEN** `alert.sh` SHALL run

#### Scenario: Job hooks still run
- **WHEN** a job with its own `failure` hook fails and a `notify.failure` command is configured
- **THEN** both the job's `failure` hook and the notification command SHALL run

#### Scenario: Skipped job produces no notification
- **WHEN** `tick` skips a job because it is already running
- **THEN** no notification command SHALL run for it

### Requirement: Failure Notification Is Limited to Once a Day
The `failure` commands SHALL run when a job fails and its previous run did not fail, or it has never run. While the job keeps failing on later runs, they SHALL run again only when at least 24 hours have passed since they last ran successfully for that job. The limit SHALL apply to each job separately.

If any `failure` command exits non-zero, the notification SHALL be treated as not sent: the commands SHALL run again on the job's next failed run, without waiting for the 24 hours.

#### Scenario: First failure notifies
- **WHEN** a job whose previous run succeeded fails
- **THEN** the `failure` commands SHALL run

#### Scenario: Repeated failure within a day does not notify again
- **WHEN** an hourly job fails, the `failure` commands run, and the job fails again one hour later
- **THEN** the `failure` commands SHALL NOT run for the second failure

#### Scenario: Still failing a day later notifies again
- **WHEN** a job has been failing since the `failure` commands last ran 25 hours ago, and it fails again
- **THEN** the `failure` commands SHALL run

#### Scenario: Each job is limited separately
- **WHEN** job `gitea` failed and notified ten minutes ago, and job `postgres` now fails for the first time
- **THEN** the `failure` commands SHALL run for `postgres`

#### Scenario: Unsent notification is retried
- **WHEN** a job fails and its `failure` command exits non-zero, and the job fails again an hour later
- **THEN** the `failure` commands SHALL run for the second failure

#### Scenario: A job that has never succeeded
- **WHEN** a job's first ever run fails
- **THEN** the `failure` commands SHALL run

### Requirement: Recovery Notification
The `recovery` commands SHALL run when a job succeeds and its previous run failed. They SHALL NOT run for a successful run that follows another successful run, or for a job's first ever run.

#### Scenario: First success after failures
- **WHEN** a job that failed on its last three runs succeeds
- **THEN** the `recovery` commands SHALL run once

#### Scenario: Continued success does not notify
- **WHEN** a job succeeds and its previous run also succeeded
- **THEN** the `recovery` commands SHALL NOT run

#### Scenario: First ever run succeeds
- **WHEN** a job's first ever run succeeds
- **THEN** the `recovery` commands SHALL NOT run

### Requirement: Success Notification on Every Success
The `success` commands SHALL run after every successful run of every job, with no limit.

#### Scenario: Every successful run
- **WHEN** an hourly job succeeds three hours in a row and `notify.success` is configured
- **THEN** the `success` commands SHALL run after each of the three runs

### Requirement: Notification Environment
Notification commands SHALL run through the same shell as hooks, with the calling environment plus:
- `RESTOMATIC_JOB`: the job's name;
- `RESTOMATIC_OUTCOME`: `success` or `failure`;
- `RESTOMATIC_FAILED_REPOS`: a comma-separated list of repositories whose backup or retention enforcement failed, empty if none;
- `RESTOMATIC_ERROR`: a one-line description of the first failure, empty on success; and
- `RESTOMATIC_FAILING_SINCE`: for `failure` and `recovery` commands, the time of the first failed run of the current or just-ended run of failures, as an RFC 3339 timestamp in UTC. Empty for `success` commands.

#### Scenario: Failure command sees the job and error
- **WHEN** the `gitea` job fails on repository `offsite`
- **THEN** the `failure` commands SHALL see `RESTOMATIC_JOB=gitea`, `RESTOMATIC_OUTCOME=failure`, `RESTOMATIC_FAILED_REPOS=offsite`, a non-empty `RESTOMATIC_ERROR`, and `RESTOMATIC_FAILING_SINCE` set to the time of this run

#### Scenario: Failing-since stays at the first failure
- **WHEN** a job first failed on Monday at 02:00 and is still failing when the `failure` commands run again on Tuesday
- **THEN** they SHALL see `RESTOMATIC_FAILING_SINCE` set to Monday 02:00

#### Scenario: Recovery command sees when the failures began
- **WHEN** a job that has been failing since Monday 02:00 succeeds
- **THEN** the `recovery` commands SHALL see `RESTOMATIC_OUTCOME=success` and `RESTOMATIC_FAILING_SINCE` set to Monday 02:00

### Requirement: Notifications Run Last and Never Change the Outcome
Notification commands SHALL run after the job's own `always` hooks and its `success` or `failure` hooks. On a successful run, `recovery` commands SHALL run before `success` commands. Within each list every command SHALL run even if an earlier one fails.

A notification command that exits non-zero SHALL be reported in the job's output, and SHALL NOT change the outcome reported or recorded for the job.

When a run is interrupted, notification commands SHALL be subject to the same time limit as the job's cleanup hooks.

#### Scenario: Order on failure
- **WHEN** a job with `always` and `failure` hooks fails and `notify.failure` is configured
- **THEN** the job's `always` hooks SHALL finish, then its `failure` hooks, then the `failure` notification commands

#### Scenario: Failing notification does not fail the job
- **WHEN** a job succeeds and a `success` notification command exits non-zero
- **THEN** the job SHALL be reported and recorded as successful, and the command's failure SHALL be reported in the job's output

#### Scenario: Interrupted run notifies as a failure
- **WHEN** a job whose previous run succeeded is interrupted by SIGTERM
- **THEN** the `failure` notification commands SHALL run after the job's cleanup hooks, within the cleanup time limit

### Requirement: Failing State Is Recorded
The state file SHALL record, for each job that is failing, the time of the first failed run of the current run of failures, and the time the `failure` commands last ran successfully for it. Both SHALL be cleared when the job next succeeds.

A job whose state was recorded by an earlier version, and whose last outcome is failed, SHALL be treated as failing with no notification yet sent.

#### Scenario: Cleared on success
- **WHEN** a failing job succeeds
- **THEN** the state file SHALL no longer record a failing-since time or a last notification time for it

#### Scenario: Job already failing before upgrade
- **WHEN** a state file written by an earlier version records a job's last outcome as failed, and the job fails again
- **THEN** the `failure` commands SHALL run
