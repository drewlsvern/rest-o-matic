## ADDED Requirements

### Requirement: Running Jobs Record Their Progress
While a job runs, it SHALL record its progress where `status` can read it, from any process, without taking the state file's lock. The record SHALL hold:

- the phase: running before hooks, backing up to a repository (named, with its position and the number of repositories, such as 1 of 2), enforcing retention on a repository, or running after hooks;
- during a backup, restic's latest progress figures as restic reports them: percent done, bytes and files done and in total, the estimated time left when restic gives one, and the files being read;
- when restic last reported, and when its bytes or files done last increased.

The record SHALL be updated at each change of phase and, during a backup, at most every 5 seconds. It SHALL be removed when the job ends, however it ends, where the process gets the chance; a record left by a killed process SHALL NOT be shown (see job-status). Recording progress SHALL NOT change what restic is given, the snapshot taken, or the job's outcome. A failure to record progress SHALL NOT fail the job.

#### Scenario: Backup in progress
- **WHEN** the `config` job has been backing up to `s3`, the first of its two repositories, for an hour
- **THEN** its record SHALL show it backing up to `s3`, 1 of 2, with restic's figures from at most about 5 seconds ago

#### Scenario: In a hook
- **WHEN** the `mssql-dev` job is running its before hooks
- **THEN** its record SHALL show the before-hooks phase and no backup figures

#### Scenario: Figures stop moving
- **WHEN** restic keeps reporting but its bytes and files done have not increased for 10 minutes
- **THEN** the record SHALL keep updating when restic last reported, and SHALL keep the time the figures last increased at 10 minutes ago

#### Scenario: Job ends
- **WHEN** a job finishes, fails, or is interrupted and runs its cleanup
- **THEN** its progress record SHALL be removed

#### Scenario: Wrapped read mode
- **WHEN** a `podman-unshare` job is backing up
- **THEN** its progress SHALL be recorded as for a `direct` job
