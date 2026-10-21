## MODIFIED Requirements

### Requirement: Plain Output Unchanged
When colour is disabled on a stream, everything rest-o-matic writes to that stream SHALL be byte-for-byte identical to its output before this capability existed, with no escape sequences, apart from progress lines (see Progress Output From run and tick).

#### Scenario: Scheduled run output unchanged
- **WHEN** `tick` runs under a scheduler with stdout and stderr redirected to a log, with no `--color` flag
- **THEN** the log SHALL contain no escape sequences, and apart from progress lines SHALL match the text rest-o-matic produced before colour support

#### Scenario: Never overrides a terminal
- **WHEN** a command runs with `--color=never` on a terminal
- **THEN** no escape sequences SHALL be emitted on either stream

## ADDED Requirements

### Requirement: Progress Output From run and tick
While `run` or `tick` is running a job, it SHALL report the job's progress on standard error. Standard output SHALL be unchanged by this requirement.

When standard error is a terminal, a single line SHALL be redrawn in place showing each running job's phase and, during a backup, the repository, percent done, bytes done and in total, and the time left. It SHALL be redrawn at most a few times a second, and SHALL be cleared before rest-o-matic writes any other line, so that it never splits one.

When standard error is not a terminal, plain lines SHALL be written instead, each a complete line starting with the time and the job's name: one at every change of phase, and one for each job in a backup at every `progress_interval` (see config). A plain line SHALL contain no carriage returns or escape sequences.

#### Scenario: Terminal
- **WHEN** a user runs `run config` on a terminal
- **THEN** one line on standard error SHALL be redrawn in place with the job's progress, and SHALL be gone when the result is printed

#### Scenario: Redirected to a file
- **WHEN** `run config 2>> run.log` runs a backup for 20 minutes with the default `progress_interval`
- **THEN** `run.log` SHALL hold a line for each phase change and about four timed progress lines, each a complete line with the time and `config`

#### Scenario: Several jobs in one tick
- **WHEN** `tick` runs `config` and `development` at the same time with standard error redirected
- **THEN** each line SHALL name one job, and lines for the two jobs SHALL never be mixed within a line

#### Scenario: Standard output unchanged
- **WHEN** `run config > result.txt` completes
- **THEN** `result.txt` SHALL hold exactly the result lines it held before this requirement
