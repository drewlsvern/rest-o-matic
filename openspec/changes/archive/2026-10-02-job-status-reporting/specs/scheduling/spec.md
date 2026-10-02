## ADDED Requirements

### Requirement: Run Record Details
For each job execution, whether started by `tick` or by `run`, the state file SHALL record:
- the time the execution started and the time it finished;
- its outcome;
- how it was started (`tick` or `run`);
- a one-line description of its first failure, empty on success; and
- for each repository the execution attempted, the result (succeeded, backup failed, or retention enforcement failed), the error for a failed result, and the ID of the snapshot created, if any.

#### Scenario: Successful run records snapshot IDs
- **WHEN** the `documents` job backs up to `nas` and `offsite` and both succeed
- **THEN** its record SHALL show a successful outcome, an empty error, and for each of `nas` and `offsite` a succeeded result with the ID of the snapshot created there

#### Scenario: Partly failed run records which repository failed
- **WHEN** the `gitea` job succeeds on `nas` and its backup to `offsite` fails
- **THEN** its record SHALL show a failed outcome, a succeeded result with a snapshot ID for `nas`, a backup-failed result with the error for `offsite`, and a one-line error naming `offsite`

#### Scenario: Before-hook failure records no repositories
- **WHEN** a job's `before` hook fails
- **THEN** its record SHALL show a failed outcome with the hook's error and no repository results

#### Scenario: Trigger is recorded
- **WHEN** a job is started with `run`
- **THEN** its record SHALL show that it was started by `run`

### Requirement: Bounded Run History
The state file SHALL keep the records of each job's 20 most recent executions, newest first. When a job has more than 20, the oldest SHALL be dropped.

#### Scenario: Oldest record dropped
- **WHEN** a job with 20 recorded executions finishes another
- **THEN** the state file SHALL hold 20 records for that job, the newest first, and the previously oldest record SHALL no longer be present

### Requirement: Tick Time Recorded
Each `tick` that reaches the point of evaluating job schedules SHALL record its time in the state file, whether or not any job is due. A `tick` that stops earlier, because the config is invalid or the state directory check fails, SHALL NOT record a time.

#### Scenario: Tick with nothing due
- **WHEN** `tick` runs and no job is due
- **THEN** the state file SHALL show that tick's time as the last tick

#### Scenario: Refused tick records nothing
- **WHEN** `tick` runs against an invalid config
- **THEN** the recorded last tick time SHALL be unchanged

### Requirement: Concurrent State Updates Preserve Every Record
When two or more processes update the state file at overlapping times, every update SHALL be preserved. No process's update SHALL be lost because another process read the file before that update and wrote it afterwards.

#### Scenario: Two jobs finish in different processes at the same time
- **WHEN** a `tick` finishes job A at the same moment as a separate `run` finishes job B
- **THEN** the state file SHALL afterwards contain both the new record for A and the new record for B

#### Scenario: Tick time written while a job finishes
- **WHEN** a new `tick` records its time at the same moment as an earlier tick records a finished job
- **THEN** the state file SHALL afterwards contain both the new tick time and the job's record

### Requirement: State Write Failures Are Reported
If a run record or tick time cannot be written to the state file, rest-o-matic SHALL print a warning naming the cause. The failure SHALL NOT change the outcome reported for the job.

#### Scenario: State file cannot be written
- **WHEN** a job succeeds and its record cannot be written because the state directory has become read-only
- **THEN** the job SHALL still be reported as successful, and a warning SHALL say that its state could not be saved

### Requirement: Earlier State Files Remain Valid
A state file written by an earlier version, holding only each job's last run time and outcome, SHALL be read without error. Due-ness SHALL be computed from it exactly as before, and its last run time and outcome SHALL be preserved until the job next runs.

#### Scenario: Upgrade does not re-run jobs
- **WHEN** a state file written by an earlier version shows a daily job last ran two hours ago, and `tick` runs with the new version
- **THEN** the job SHALL NOT be due

#### Scenario: Earlier record is still shown
- **WHEN** `status` is run against a state file written by an earlier version
- **THEN** each job SHALL be shown with its last run time and outcome, and with no duration or error
