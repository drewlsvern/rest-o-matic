## MODIFIED Requirements

### Requirement: FIFO Queuing of Deferred Jobs
A job that cannot start immediately because of the per-repository rule or the global cap SHALL be queued rather than skipped or dropped, and SHALL be started, in order of when it became due, as soon as it is no longer blocked by either rule.

This SHALL hold whether the execution blocking it belongs to the same process or to another one, and whether the job was started by `tick` or by `run`. While a job is queued:
- none of its hooks SHALL run and no restic command SHALL be run for it;
- it SHALL NOT be reported or recorded as failed because it had to wait; and
- the command SHALL say which job is waiting.

A job SHALL NOT begin until every repository it backs up to is free and a concurrency slot is available; once it has begun, it SHALL NOT be held up part-way by another job using one of its repositories.

If the process is interrupted (SIGTERM or SIGINT) while a job is queued, that job SHALL be treated as not started: none of its hooks SHALL run and no run SHALL be recorded for it.

#### Scenario: Deferred job runs once a slot frees up
- **WHEN** a job is deferred due to the global concurrency cap
- **THEN** it SHALL automatically start as soon as a running execution completes and a slot becomes available, without requiring a new tick

#### Scenario: Queue order follows due order
- **WHEN** two jobs are both deferred and later become eligible to run at the same moment
- **THEN** the job that became due earlier SHALL be started first

#### Scenario: Two jobs sharing a repository both back up on one tick
- **WHEN** the daily jobs `documents` and `postgres` both back up to `nas` and both become due on the same tick
- **THEN** one SHALL run first, the other SHALL start as soon as the first has finished, both SHALL be recorded as successful, and `nas` SHALL hold a new snapshot from each

#### Scenario: Nothing runs while a job waits
- **WHEN** a job whose `before` hook stops a container is waiting for a repository that another job is using
- **THEN** the `before` hook SHALL NOT run, and the container SHALL keep running, until the repository is free

#### Scenario: Waiting for a job started by an earlier tick
- **WHEN** an earlier tick's job is still backing up to `nas`, and a later tick finds a different job due that also backs up to `nas`
- **THEN** the later tick SHALL wait, and SHALL start that job once the earlier one has finished

#### Scenario: Waiting is not a failure
- **WHEN** a job waits for a repository and then backs up successfully
- **THEN** it SHALL be reported and recorded as successful, its success hooks SHALL run, and the command SHALL exit with status zero

#### Scenario: Job with two repositories waits for both
- **WHEN** a job backs up to `nas` and `offsite`, and another job is using `offsite`
- **THEN** the job SHALL NOT start its hooks or its backup to `nas` until `offsite` is also free

#### Scenario: Interrupted while waiting
- **WHEN** `tick` receives SIGTERM while a due job is waiting for a repository
- **THEN** none of that job's hooks SHALL run, no run SHALL be recorded for it, and it SHALL still be due on the next tick

### Requirement: Cross-Tick Running-Job Detection
Because a job's execution may outlive the interval between tick invocations, the system SHALL be able to determine, at the start of any tick, whether a given job or job-repository pair is still running from a previous tick, in order to avoid starting a duplicate execution of the same job.

A job that is already running, or already queued and waiting to start, whether through `tick` or through `run`, SHALL NOT be started or queued again by either command. For such a job:
- none of its hooks SHALL run;
- no run SHALL be recorded for it, and its recorded state SHALL be left for the existing execution to write;
- `tick` SHALL report it as already running, SHALL NOT count it as failed, and SHALL NOT exit non-zero because of it; and
- `run` SHALL fail with a non-zero exit status and a message saying the job is already running.

The detection SHALL NOT be fooled by an execution whose process was killed: once that process is gone, the job SHALL be startable again without any manual cleanup.

#### Scenario: A new tick does not duplicate a still-running job
- **WHEN** a job started by an earlier tick has not yet finished and the job becomes due again on a later tick
- **THEN** the later tick SHALL detect the job is still running and SHALL NOT start a second, concurrent execution of that same job

#### Scenario: Hooks do not run for an already-running job
- **WHEN** a job whose `before` hook stops a container and whose `always` hook starts it is still backing up, and a later tick finds it due
- **THEN** the later tick SHALL run neither hook, and the container SHALL stay stopped until the running execution's own `always` hook starts it

#### Scenario: Skipped job is not a failure
- **WHEN** a tick's only due job is still running from an earlier tick
- **THEN** the tick SHALL report that job as already running, SHALL run no failure hooks, and SHALL exit with status zero

#### Scenario: Ticks do not pile up behind a waiting job
- **WHEN** one tick is waiting to start a job, and the next three ticks each find that job still due
- **THEN** each of those three ticks SHALL report the job as already running and exit, and only the first tick SHALL run the job once it can start

#### Scenario: A job run by another tick meanwhile is not repeated
- **WHEN** a tick finds a job due but cannot start it straight away, and before it can, a later tick runs that job to completion
- **THEN** the first tick SHALL NOT run the job again for the same schedule boundary

#### Scenario: Other due jobs still run
- **WHEN** a tick finds two jobs due, one of which is still running from an earlier tick
- **THEN** the tick SHALL skip the running job and SHALL run the other

#### Scenario: Manual run of a running job is refused
- **WHEN** `run gitea` is invoked while `gitea` is running from a tick
- **THEN** it SHALL exit non-zero with a message that `gitea` is already running, without running any hook

#### Scenario: Killed execution does not block later runs
- **WHEN** the process running a job is killed with SIGKILL, and a later tick finds the job due
- **THEN** the later tick SHALL start the job
