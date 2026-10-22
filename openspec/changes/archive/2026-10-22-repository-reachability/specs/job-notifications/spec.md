## MODIFIED Requirements

### Requirement: Notification Environment
Notification commands SHALL run through the same shell as hooks, with the calling environment plus:
- `RESTOMATIC_JOB`: the job's name;
- `RESTOMATIC_OUTCOME`: `success` or `failure`;
- `RESTOMATIC_FAILED_REPOS`: a comma-separated list of repositories whose backup or retention enforcement failed, or that were unreachable, empty if none;
- `RESTOMATIC_UNREACHABLE_REPOS`: a comma-separated list of the repositories that were unreachable, empty if none;
- `RESTOMATIC_ERROR`: a one-line description of the first failure, empty on success; and
- `RESTOMATIC_FAILING_SINCE`: for `failure` and `recovery` commands, the time of the first failed run of the current or just-ended run of failures, as an RFC 3339 timestamp in UTC. Empty for `success` commands.

#### Scenario: Failure command sees the job and error
- **WHEN** the `gitea` job fails on repository `offsite`
- **THEN** the `failure` commands SHALL see `RESTOMATIC_JOB=gitea`, `RESTOMATIC_OUTCOME=failure`, `RESTOMATIC_FAILED_REPOS=offsite`, a non-empty `RESTOMATIC_ERROR`, and `RESTOMATIC_FAILING_SINCE` set to the time of this run

#### Scenario: Failure command sees an unreachable repository
- **WHEN** the `config` job succeeds on `ut-g5` and finds `s3` unreachable
- **THEN** the `failure` commands SHALL see `RESTOMATIC_FAILED_REPOS=s3`, `RESTOMATIC_UNREACHABLE_REPOS=s3`, and a `RESTOMATIC_ERROR` naming `s3` as unreachable

#### Scenario: Failing-since stays at the first failure
- **WHEN** a job first failed on Monday at 02:00 and is still failing when the `failure` commands run again on Tuesday
- **THEN** they SHALL see `RESTOMATIC_FAILING_SINCE` set to Monday 02:00

#### Scenario: Recovery command sees when the failures began
- **WHEN** a job that has been failing since Monday 02:00 succeeds
- **THEN** the `recovery` commands SHALL see `RESTOMATIC_OUTCOME=success` and `RESTOMATIC_FAILING_SINCE` set to Monday 02:00
