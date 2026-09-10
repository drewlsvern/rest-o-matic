# backup-execution Specification

## Purpose

Defines the observable behavior of running a single backup job end-to-end: executing hooks, invoking restic against each configured repository, tagging snapshots so shared repositories stay safe, and enforcing retention afterward.

## Requirements

### Requirement: Job-Level Hook Execution
A job's `before` hooks SHALL run once per job execution, prior to any repository backup. A job's `after` hooks SHALL run once per job execution, after all configured repositories have been attempted. Hooks SHALL NOT be re-run per repository.

#### Scenario: Hooks run once regardless of repository count
- **WHEN** a job with `before` and `after` hooks lists two repositories
- **THEN** the before hooks SHALL execute exactly once, followed by a backup attempt against each of the two repositories, followed by the after hooks executing exactly once

### Requirement: Before-Hook Failure Aborts Backup
If any `before` hook exits with a non-zero status, the job SHALL NOT proceed to back up any repository. The job's `after` hooks SHALL still run (best-effort cleanup) before the job is reported as failed.

#### Scenario: Failed before hook skips all repository backups
- **WHEN** a job's before hook exits non-zero
- **THEN** no `restic backup` invocation SHALL be attempted for any of the job's repositories, the after hooks SHALL still run, and the job's outcome SHALL be recorded as failed

### Requirement: Independent Per-Repository Backup
After hooks succeed, the job SHALL invoke a backup against each of its configured repositories independently. A failure backing up to one repository SHALL NOT prevent the job from attempting the remaining repositories.

#### Scenario: One repository failing does not block another
- **WHEN** a job has two repositories and the backup to the first fails (e.g. unreachable)
- **THEN** the job SHALL still attempt the backup to the second repository, and the job's overall outcome SHALL reflect that at least one repository failed

### Requirement: Automatic Job Tagging
Every snapshot created by a job's backup SHALL be tagged with that job's name, unconditionally. This tag SHALL NOT be omitted, disabled, or replaced by user configuration.

#### Scenario: Snapshot always carries the job-name tag
- **WHEN** the `postgres` job backs up to any of its repositories
- **THEN** the resulting snapshot SHALL carry a tag equal to `postgres`

### Requirement: Additive User Tags
A job MAY declare additional user-supplied tags. These tags SHALL be applied in addition to the automatic job-name tag, never in place of it.

#### Scenario: User tag added alongside the job-name tag
- **WHEN** a job declares `tags: [prod]`
- **THEN** the resulting snapshot SHALL carry both the `prod` tag and the automatic job-name tag

### Requirement: Tag-Scoped Retention Enforcement
After backup(s) complete, the job SHALL enforce retention against each (job, repository) pair by removing snapshots outside the resolved retention rules, scoped to snapshots carrying that job's automatic tag. Retention enforcement SHALL NOT consider or remove snapshots belonging to a different job, even when those snapshots reside in the same repository.

#### Scenario: Retention on a shared repository only affects its own job's snapshots
- **WHEN** repository `nas` holds snapshots from both the `documents` job and the `postgres` job, and the `postgres` job's retention enforcement runs
- **THEN** only snapshots tagged `postgres` SHALL be considered for removal, and snapshots tagged `documents` SHALL be unaffected

#### Scenario: Retention uses the resolved per-repository effective retention
- **WHEN** a job's repository has a per-repository retention override
- **THEN** retention enforcement for that repository SHALL use the overridden effective retention, not the job's or policy's unoverridden retention

### Requirement: On-Demand Job Execution
A user SHALL be able to trigger execution of a specific named job directly, independent of whether that job is currently due according to its schedule.

#### Scenario: Manually running a job bypasses due-checking
- **WHEN** a user directly invokes execution of a specific job
- **THEN** the job SHALL run its full hook/backup/retention sequence regardless of its schedule's due state
