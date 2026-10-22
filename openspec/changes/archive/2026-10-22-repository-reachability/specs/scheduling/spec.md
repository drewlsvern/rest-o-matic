## MODIFIED Requirements

### Requirement: Run Record Details
For each job execution, whether started by `tick` or by `run`, the state file SHALL record:
- the time the execution started and the time it finished;
- its outcome;
- how it was started (`tick` or `run`);
- a one-line description of its first failure, empty on success; and
- for each repository the execution attempted, the result (succeeded, unreachable, backup failed, or retention enforcement failed), the error for a result other than succeeded, and the ID of the snapshot created, if any.

#### Scenario: Successful run records snapshot IDs
- **WHEN** the `documents` job backs up to `nas` and `offsite` and both succeed
- **THEN** its record SHALL show a successful outcome, an empty error, and for each of `nas` and `offsite` a succeeded result with the ID of the snapshot created there

#### Scenario: Partly failed run records which repository failed
- **WHEN** the `gitea` job succeeds on `nas` and its backup to `offsite` fails
- **THEN** its record SHALL show a failed outcome, a succeeded result with a snapshot ID for `nas`, a backup-failed result with the error for `offsite`, and a one-line error naming `offsite`

#### Scenario: Unreachable repository is recorded as such
- **WHEN** the `config` job succeeds on `ut-g5` and finds `s3` unreachable
- **THEN** its record SHALL show a failed outcome, a succeeded result with a snapshot ID for `ut-g5`, and an unreachable result with its error and no snapshot ID for `s3`

#### Scenario: Before-hook failure records no repositories
- **WHEN** a job's `before` hook fails
- **THEN** its record SHALL show a failed outcome with the hook's error and no repository results

#### Scenario: Trigger is recorded
- **WHEN** a job is started with `run`
- **THEN** its record SHALL show that it was started by `run`
