## ADDED Requirements

### Requirement: Progress Interval
The config MAY declare a top-level `progress_interval`: how often `run` and `tick` write a plain progress line for each running job when their standard error is not a terminal. It SHALL be a duration written as a number followed by `s`, `m` or `h` (such as `30s`, `5m` or `1h`), or `0`. Omitting it SHALL be the same as `5m`. `0` SHALL turn the timed lines off, leaving the lines written when a job changes phase. A value that is not a valid duration, or a duration below `10s` other than `0`, SHALL be a config validation error identifying the key. The interval SHALL NOT affect how often a job records its progress for `status`, nor the progress line redrawn on a terminal.

#### Scenario: Default
- **WHEN** the config does not declare `progress_interval`
- **THEN** a plain progress line SHALL be written for each running job every 5 minutes

#### Scenario: Custom interval
- **WHEN** the config declares `progress_interval: 1m`
- **THEN** a plain progress line SHALL be written for each running job every minute

#### Scenario: Timed lines off
- **WHEN** the config declares `progress_interval: 0`
- **THEN** no timed progress line SHALL be written, and a line SHALL still be written at each change of phase

#### Scenario: Invalid value
- **WHEN** the config declares `progress_interval: often` or `progress_interval: 2s`
- **THEN** config validation SHALL fail with an error identifying `progress_interval`
