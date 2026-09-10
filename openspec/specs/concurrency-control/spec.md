# concurrency-control Specification

## Purpose

Defines the two-tier execution guard applied to the set of due jobs, ensuring restic's own repository-level locking is never violated and that host resource usage stays within a configurable bound, even across separate tick invocations.

## Requirements

### Requirement: Per-Repository Mutual Exclusion
Two job executions that target the same repository SHALL NOT run concurrently against it. This SHALL hold even when the two executions were triggered by different jobs, and even when one execution was started by a prior tick invocation and is still running when a new tick occurs.

#### Scenario: Two jobs sharing a repository do not run concurrently
- **WHEN** job A and job B both target repository `nas` and both become due at the same tick
- **THEN** the system SHALL run at most one of them against `nas` at a time, deferring the other until the first finishes

#### Scenario: A still-running job from a prior tick blocks a new execution against the same repository
- **WHEN** a tick starts a job against repository `nas` that takes longer than the interval before the next tick occurs, and another job targeting `nas` becomes due on that next tick
- **THEN** the newly due job SHALL be deferred rather than started concurrently against `nas`

### Requirement: Global Concurrency Cap
The total number of job executions running at the same time, across all repositories, SHALL NOT exceed a configurable maximum. This cap SHALL apply regardless of how many jobs are simultaneously due.

#### Scenario: Due jobs beyond the cap are deferred
- **WHEN** the configured maximum concurrent executions is 2 and 4 jobs become due at the same tick with no repository overlap between them
- **THEN** at most 2 SHALL run at the same time, with the remaining 2 deferred until a running slot frees up

### Requirement: FIFO Queuing of Deferred Jobs
A job that cannot start immediately because of the per-repository rule or the global cap SHALL be queued rather than skipped or dropped, and SHALL be started, in order of when it became due, as soon as it is no longer blocked by either rule.

#### Scenario: Deferred job runs once a slot frees up
- **WHEN** a job is deferred due to the global concurrency cap
- **THEN** it SHALL automatically start as soon as a running execution completes and a slot becomes available, without requiring a new tick

#### Scenario: Queue order follows due order
- **WHEN** two jobs are both deferred and later become eligible to run at the same moment
- **THEN** the job that became due earlier SHALL be started first

### Requirement: Cross-Tick Running-Job Detection
Because a job's execution may outlive the interval between tick invocations, the system SHALL be able to determine, at the start of any tick, whether a given job or job-repository pair is still running from a previous tick, in order to avoid starting a duplicate execution of the same job.

#### Scenario: A new tick does not duplicate a still-running job
- **WHEN** a job started by an earlier tick has not yet finished and the job becomes due again on a later tick
- **THEN** the later tick SHALL detect the job is still running and SHALL NOT start a second, concurrent execution of that same job
