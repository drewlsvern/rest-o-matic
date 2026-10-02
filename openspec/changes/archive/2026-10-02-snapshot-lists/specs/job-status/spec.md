## ADDED Requirements

### Requirement: Snapshot Lists Are Shown
`status <job>` SHALL show, for each repository the job backs up to, the snapshots recorded for that job there, newest first, with each snapshot's ID, time, size and what it changed, and the time the list was taken. A repository with no recorded list SHALL be shown as not listed yet.

In the JSON output, every job SHALL carry an entry for each repository it backs up to, giving when the list was taken, how many snapshots it holds and the time of the newest, with nulls where no list has been recorded. When a single job is requested, each entry SHALL also carry the snapshots themselves.

Showing snapshot lists SHALL NOT invoke restic or contact any repository.

#### Scenario: Snapshots listed for a job
- **WHEN** `status documents` is run and three snapshots are recorded for `documents` in `nas`
- **THEN** all three SHALL be shown, newest first, with the time the list was taken

#### Scenario: Repository not listed yet
- **WHEN** `status documents` is run and the job has never completed a run against `offsite`
- **THEN** `offsite` SHALL be shown as not listed yet

#### Scenario: Summary for every job
- **WHEN** `status --json` is run
- **THEN** each job SHALL carry, for each of its repositories, the list's time, its snapshot count and the newest snapshot's time, and SHALL NOT carry the snapshots themselves

#### Scenario: Full list for one job
- **WHEN** `status documents --json` is run
- **THEN** each of the job's repository entries SHALL carry its snapshots, each with its ID, time, host, paths, tags, and its size, file count and what it changed, or null where those are unknown

#### Scenario: Works with the repository unreachable
- **WHEN** `status documents` is run while the job's repository can't be reached
- **THEN** the recorded list SHALL be shown, and restic SHALL NOT be invoked
