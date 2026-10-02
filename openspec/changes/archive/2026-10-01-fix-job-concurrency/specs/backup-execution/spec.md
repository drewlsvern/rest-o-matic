## MODIFIED Requirements

### Requirement: Job Outcome Selects Success or Failure Hooks
A job SHALL be considered successful only if its `before` hooks succeeded, every configured repository's backup and retention enforcement succeeded, and every `always` command succeeded. Otherwise it SHALL be considered failed. This includes an execution interrupted by a signal. The `success` hooks SHALL run only for a successful job, and the `failure` hooks only for a failed one. A failing `success` or `failure` command SHALL be reported but SHALL NOT change the job's outcome.

#### Scenario: Failed always command fails the job
- **WHEN** every repository backup succeeds but an `always` command exits non-zero
- **THEN** the job SHALL be reported and recorded as failed, and the failure hooks SHALL run instead of the success hooks

#### Scenario: One failed repository fails the job
- **WHEN** a job has two repositories and only one of the backups fails
- **THEN** the failure hooks SHALL run and the success hooks SHALL NOT

#### Scenario: Failing outcome hook does not change the outcome
- **WHEN** a job fully succeeds and one of its success commands exits non-zero
- **THEN** the job SHALL still be recorded as successful, and the command's failure SHALL be reported in the job's output

### Requirement: Hook Environment
Every hook command SHALL run with the calling environment plus `RESTOMATIC_JOB` set to the job's name. `success` and `failure` commands SHALL additionally receive:
- `RESTOMATIC_OUTCOME`: `success` or `failure`;
- `RESTOMATIC_FAILED_REPOS`: a comma-separated list of repositories whose backup or retention enforcement failed, empty if none; and
- `RESTOMATIC_ERROR`: a one-line description of the first failure, empty on success.

#### Scenario: Failure hook sees the outcome and failed repository
- **WHEN** a job backing up to repositories `nas` and `s3` fails only on `s3`
- **THEN** its failure commands SHALL see `RESTOMATIC_JOB` set to the job's name, `RESTOMATIC_OUTCOME=failure`, `RESTOMATIC_FAILED_REPOS=s3`, and a non-empty `RESTOMATIC_ERROR`

#### Scenario: Before hook sees the job name
- **WHEN** a job's before hook runs
- **THEN** it SHALL see `RESTOMATIC_JOB` set to the job's name
