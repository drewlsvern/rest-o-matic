# snapshot-lists Specification

## Purpose

Keeps a local record of the snapshots each job has in each repository, so that they can be shown instantly and for every repository at once, without contacting any backend.

## Requirements

### Requirement: Snapshot List Recorded After Each Run
When a job's retention enforcement for a repository succeeds, rest-o-matic SHALL record the snapshots that job has in that repository afterwards, together with the time the list was taken. The list SHALL contain only that job's own snapshots, never another job's in the same repository. For each snapshot it SHALL record the snapshot's ID, its time, the host it was taken on, its paths and its tags. When restic reports them, it SHALL also record the snapshot's size and file count, and what the snapshot changed compared with the one before it: the number of files that were new, the number that had changed, and the amount of data it added to the repository.

The list SHALL be taken from the retention enforcement step. rest-o-matic SHALL NOT invoke restic an additional time to obtain it.

#### Scenario: List recorded after a successful run
- **WHEN** the `documents` job backs up to `nas` and retention leaves it with three snapshots there
- **THEN** the recorded list for `documents` in `nas` SHALL hold those three snapshots, including the one just created, with the time the list was taken

#### Scenario: Pruned snapshots leave the list
- **WHEN** retention removes a job's oldest snapshot from a repository
- **THEN** that snapshot SHALL NOT be in the recorded list

#### Scenario: Only the job's own snapshots
- **WHEN** repository `nas` holds snapshots from both `documents` and `postgres`, and `documents` runs
- **THEN** the list recorded for `documents` in `nas` SHALL contain no `postgres` snapshot

#### Scenario: No extra restic invocation
- **WHEN** a job backs up to one repository
- **THEN** restic SHALL be invoked once for the backup and once for retention enforcement, and no further time

#### Scenario: What a snapshot changed
- **WHEN** a job's second run finds 12 new files and 3 changed ones, on a restic that reports this
- **THEN** the snapshot recorded for that run SHALL carry 12 new files, 3 changed files, and the amount of data it added

#### Scenario: Figures unknown on an older restic
- **WHEN** the installed restic does not report snapshot sizes or what a snapshot changed
- **THEN** the list SHALL still be recorded, with those figures unknown

### Requirement: Lists Are Kept Separately per Job and Repository
A list SHALL be recorded for each pairing of a job and a repository it backs up to. Refreshing one SHALL NOT alter another.

#### Scenario: One repository fails
- **WHEN** a job backs up to `nas` and `offsite`, succeeds on `nas`, and fails on `offsite`
- **THEN** the list for `nas` SHALL be refreshed, and the list for `offsite` SHALL be left exactly as it was, with its earlier time

### Requirement: A Failed Run Keeps the Previous List
If a job's backup or retention enforcement fails for a repository, or the run never reaches that repository, the list previously recorded for that job and repository SHALL be kept unchanged, including the time it was taken.

#### Scenario: Backup fails
- **WHEN** a job that has a recorded list for `nas` fails its next backup to `nas`
- **THEN** the recorded list and its time SHALL be unchanged

#### Scenario: Before hook fails
- **WHEN** a job's `before` hook fails, so no repository is attempted
- **THEN** every list recorded for that job SHALL be unchanged

### Requirement: An Unreadable List Never Fails the Job
If the snapshots can't be read from the retention step's output, or the list can't be saved, the job's outcome SHALL NOT change. The previous list SHALL be kept, and a failure to save SHALL be reported as a warning.

#### Scenario: Output can't be read
- **WHEN** retention enforcement succeeds but its output contains no snapshot list
- **THEN** the job SHALL be reported as successful and the previous list SHALL be kept
