## ADDED Requirements

### Requirement: Repositories Are Checked Before Backing Up
Before each backup to a repository, unless its `connect_timeout` is `0`, the job SHALL check that the repository can be opened, by asking restic to read the repository's config, with restic's usual credentials and the job's read mode, and without taking a repository lock. The check SHALL be stopped when it has not finished within the repository's `connect_timeout`.

- If the check succeeds, the backup SHALL go ahead as it would have without a check.
- If the check is stopped because time ran out, the repository's result SHALL be **unreachable**, its backup and retention enforcement SHALL be skipped, and the job SHALL go on to its next repository. The error recorded SHALL be the last retry message restic printed during the check, in restic's own words without restic's retry bookkeeping, or, if it printed none, a message saying that the repository could not be opened within the timeout.
- If restic fails within the time limit, the repository's result SHALL be backup failed, with restic's error reported as for a failed backup, and its backup SHALL be skipped.

An unreachable repository SHALL fail the job as a failed backup does.

#### Scenario: Endpoint not answering
- **WHEN** a job backs up to `s3` and `ut-g5`, `s3` has the default `connect_timeout` and its endpoint doesn't answer
- **THEN** `s3` SHALL be recorded as unreachable after about 2 minutes, with restic's last retry message as its error, the backup to `ut-g5` SHALL then run, and the job SHALL be recorded as failed

#### Scenario: Wrong password
- **WHEN** the check against a repository fails at once because the password is wrong
- **THEN** the repository SHALL be recorded as backup failed with restic's message, not as unreachable

#### Scenario: Reachable repository
- **WHEN** the check against a repository succeeds
- **THEN** the backup SHALL run with the same arguments as without a check

#### Scenario: Check turned off
- **WHEN** a repository's `connect_timeout` is `0`
- **THEN** its backup SHALL start without a check, as before this requirement

### Requirement: restic's Retry Messages Are Recorded
While restic runs for a job's check or backup, rest-o-matic SHALL read restic's standard error as restic writes it. A line in which restic reports that an operation failed and will be retried SHALL be recorded in the job's progress record as the latest retry message, in restic's own words without restic's retry bookkeeping, with the time it was seen. Standard error SHALL still be kept whole for reporting restic's errors, as before.

#### Scenario: Retrying a connection
- **WHEN** restic writes `Stat(<config/>) returned error, retrying after 1m13s: Stat: Get "https://s3.example.com/b/?location=": dial tcp 192.0.2.1:443: i/o timeout`
- **THEN** the progress record SHALL hold the retry message `Stat: Get "https://s3.example.com/b/?location=": dial tcp 192.0.2.1:443: i/o timeout` and when it was seen

#### Scenario: restic that doesn't print retries
- **WHEN** restic writes no retry lines
- **THEN** the progress record SHALL hold no retry message

## MODIFIED Requirements

### Requirement: Restic Failures Are Reported as Readable Messages
When a restic command run for a job fails, the error rest-o-matic reports SHALL contain restic's exit status and restic's own messages as plain text. Where restic reports an error as a JSON object, the reported error SHALL contain the message from that object and SHALL NOT contain the JSON itself. Lines restic prints as plain text SHALL be kept. A leading `Fatal: ` SHALL be dropped, and only the first line of a multi-line message SHALL be kept. A line in which restic says it will retry an operation SHALL NOT be reported as one of its messages, since it says what restic did next rather than why it failed (see restic's Retry Messages Are Recorded).

When restic reports more than a few separate errors, the first few SHALL be reported followed by a count of the rest.

This SHALL apply wherever the error surfaces: the output of `run` and `tick`, the recorded run, `status`, and the `RESTOMATIC_ERROR` given to hooks and notifications. It SHALL NOT change the output of `exec`, which remains restic's own.

#### Scenario: Repository does not exist
- **WHEN** a job backs up to a repository that was never initialised
- **THEN** the reported error SHALL say that the repository does not exist, SHALL include restic's exit status, and SHALL NOT contain `message_type` or any other JSON

#### Scenario: Wrong password
- **WHEN** a job backs up to a repository with the wrong password
- **THEN** the reported error SHALL say `wrong password or no key found`

#### Scenario: Unreadable source file
- **WHEN** a backup can't read one source file because permission is denied
- **THEN** the reported error SHALL name that file and say permission was denied, in plain text, and the hint about unreadable source files SHALL still be given

#### Scenario: Many errors are summarised
- **WHEN** a backup can't read twenty source files
- **THEN** the reported error SHALL list the first few and say how many more there were

#### Scenario: Notification text is readable
- **WHEN** a job fails because its repository does not exist and a `failure` notification command uses `RESTOMATIC_ERROR`
- **THEN** `RESTOMATIC_ERROR` SHALL be a plain sentence containing `repository does not exist`

#### Scenario: Retries before failing
- **WHEN** restic prints three retry lines and then fails with `Fatal: unable to open config file: … i/o timeout`
- **THEN** the reported error SHALL be the `unable to open config file` message, and SHALL NOT contain the retry lines


### Requirement: Running Jobs Record Their Progress
While a job runs, it SHALL record its progress where `status` can read it, from any process, without taking the state file's lock. The record SHALL hold:

- the phase: running before hooks, checking a repository, backing up to a repository, enforcing retention on a repository (each named, with its position and the number of repositories, such as 1 of 2), or running after hooks, and when the phase started;
- during a backup, restic's latest progress figures as restic reports them: percent done, bytes and files done and in total, seconds elapsed, the estimated time left when restic gives one, and the files being read;
- when restic last reported, and when its bytes or files done last increased;
- during a check or a backup, restic's latest retry message, if any, and when it was seen.

The record SHALL be updated at each change of phase, when restic prints a retry message and, during a backup, at most every 5 seconds. It SHALL be removed when the job ends, however it ends, where the process gets the chance; a record left by a killed process SHALL NOT be shown (see job-status). Recording progress SHALL NOT change what restic is given, the snapshot taken, or the job's outcome. A failure to record progress SHALL NOT fail the job.

#### Scenario: Backup in progress
- **WHEN** the `config` job has been backing up to `s3`, the first of its two repositories, for an hour
- **THEN** its record SHALL show it backing up to `s3`, 1 of 2, with restic's figures from at most about 5 seconds ago

#### Scenario: In a hook
- **WHEN** the `mssql-dev` job is running its before hooks
- **THEN** its record SHALL show the before-hooks phase and no backup figures

#### Scenario: Checking a repository
- **WHEN** the `config` job is checking `s3`, the first of its two repositories, and restic has printed a retry message
- **THEN** its record SHALL show it checking `s3`, 1 of 2, with when the check started and that retry message

#### Scenario: Figures stop moving
- **WHEN** restic keeps reporting but its bytes and files done have not increased for 10 minutes
- **THEN** the record SHALL keep updating when restic last reported, and SHALL keep the time the figures last increased at 10 minutes ago

#### Scenario: Job ends
- **WHEN** a job finishes, fails, or is interrupted and runs its cleanup
- **THEN** its progress record SHALL be removed

#### Scenario: Wrapped read mode
- **WHEN** a `podman-unshare` job is backing up
- **THEN** its progress SHALL be recorded as for a `direct` job

### Requirement: Hook Environment
Every hook command SHALL run with the calling environment plus `RESTOMATIC_JOB` set to the job's name. `success` and `failure` commands SHALL additionally receive:
- `RESTOMATIC_OUTCOME`: `success` or `failure`;
- `RESTOMATIC_FAILED_REPOS`: a comma-separated list of repositories whose backup or retention enforcement failed, or that were unreachable, empty if none;
- `RESTOMATIC_UNREACHABLE_REPOS`: a comma-separated list of the repositories that were unreachable, empty if none; and
- `RESTOMATIC_ERROR`: a one-line description of the first failure, empty on success.

#### Scenario: Failure hook sees the outcome and failed repository
- **WHEN** a job backing up to repositories `nas` and `s3` fails only on `s3`
- **THEN** its failure commands SHALL see `RESTOMATIC_JOB` set to the job's name, `RESTOMATIC_OUTCOME=failure`, `RESTOMATIC_FAILED_REPOS=s3`, and a non-empty `RESTOMATIC_ERROR`

#### Scenario: Failure hook sees an unreachable repository
- **WHEN** a job backing up to `nas` and `s3` succeeds on `nas` and finds `s3` unreachable
- **THEN** its failure commands SHALL see `RESTOMATIC_FAILED_REPOS=s3` and `RESTOMATIC_UNREACHABLE_REPOS=s3`

#### Scenario: Before hook sees the job name
- **WHEN** a job's before hook runs
- **THEN** it SHALL see `RESTOMATIC_JOB` set to the job's name
