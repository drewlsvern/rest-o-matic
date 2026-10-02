## ADDED Requirements

### Requirement: Failing Since Is Shown
For a job whose most recent run failed, `status` SHALL show the time of the first failed run of its current run of failures, in both the default output and the JSON output. For a job that is not failing, the JSON value SHALL be null. This SHALL be reported whether or not any notification commands are configured.

#### Scenario: Job failing for several runs
- **WHEN** a job has failed on every run since Monday at 02:00
- **THEN** `status` SHALL show that it has been failing since Monday at 02:00

#### Scenario: Healthy job
- **WHEN** a job's most recent run succeeded
- **THEN** `status --json` SHALL report a null failing-since time for it
