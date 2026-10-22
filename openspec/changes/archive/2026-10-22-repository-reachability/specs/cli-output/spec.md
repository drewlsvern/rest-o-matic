## MODIFIED Requirements

### Requirement: Progress Output From run and tick
While `run` or `tick` is running a job, it SHALL report the job's progress on standard error. Standard output SHALL be unchanged by this requirement.

The progress of a job SHALL be described as `status` describes it in its default output: the phase and repository, and during a backup the percent done, bytes done and in total, throughput, and the time left or the reason there is none ("no progress reported for", "no report from restic yet"), followed, when restic is retrying, by "restic is retrying:" and its message.

When standard error is a terminal, a single line SHALL be redrawn in place showing each running job's progress. It SHALL be redrawn at most a few times a second, and SHALL be cleared before rest-o-matic writes any other line, so that it never splits one.

When standard error is not a terminal, plain lines SHALL be written instead, each a complete line starting with the time and the job's name: one at every change of phase, one for each new retry message restic prints, and one for each job in a backup at every `progress_interval` (see config). A plain line SHALL contain no carriage returns or escape sequences.

#### Scenario: Terminal
- **WHEN** a user runs `run config` on a terminal
- **THEN** one line on standard error SHALL be redrawn in place with the job's progress, and SHALL be gone when the result is printed

#### Scenario: Redirected to a file
- **WHEN** `run config 2>> run.log` runs a backup for 20 minutes with the default `progress_interval`
- **THEN** `run.log` SHALL hold a line for each phase change and about four timed progress lines, each a complete line with the time and `config`

#### Scenario: Retrying while redirected
- **WHEN** `run config 2>> run.log` is checking `s3` and restic prints three retry messages
- **THEN** `run.log` SHALL hold a line for each of them, naming `config` and `s3`

#### Scenario: Several jobs in one tick
- **WHEN** `tick` runs `config` and `development` at the same time with standard error redirected
- **THEN** each line SHALL name one job, and lines for the two jobs SHALL never be mixed within a line

#### Scenario: Standard output unchanged
- **WHEN** `run config > result.txt` completes
- **THEN** `result.txt` SHALL hold exactly the result lines it held before this requirement
