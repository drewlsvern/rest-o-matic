## Purpose

Defines the scheduler-agnostic, daemon-free scheduling model: a single `tick` entry point that determines which jobs are due by comparing policy schedules against persisted last-run state, so any external periodic trigger can drive rest-o-matic without it needing to understand cron, systemd, or any other OS scheduler.

## ADDED Requirements

### Requirement: Tick as the Sole Scheduling Entry Point
The system SHALL NOT run as a persistent background process, and SHALL NOT create, modify, or otherwise manage OS-level scheduler entries (e.g. crontab lines, systemd timers). Scheduled evaluation of jobs SHALL occur only when a `tick` invocation runs, exits, and returns control to whatever external mechanism invoked it.

#### Scenario: Tick performs one evaluation pass and exits
- **WHEN** `tick` is invoked
- **THEN** the system SHALL evaluate all configured jobs for due-ness, act on the due ones, and terminate without remaining resident

#### Scenario: No crontab or scheduler files are written
- **WHEN** the system runs, at any point, including during job configuration or execution
- **THEN** it SHALL NOT write to the system crontab or create any OS scheduler unit

### Requirement: Calendar-Aligned Due Detection
A job SHALL be considered due when the current time has reached the next calendar-aligned boundary implied by its resolved schedule, relative to its last successful run. Due-ness SHALL be computed from calendar boundaries (e.g. the top of the hour for an hourly schedule), not from a fixed elapsed-duration countdown since the last run.

#### Scenario: Hourly job becomes due at the top of the hour
- **WHEN** an hourly job last ran at 1:58 and the current tick occurs at 2:00
- **THEN** the job SHALL be evaluated as due

#### Scenario: Hourly job is not due before its next boundary
- **WHEN** an hourly job last ran at 2:00 and the current tick occurs at 2:15
- **THEN** the job SHALL be evaluated as not due

### Requirement: Catch-Up After Missed Ticks
If a job's due boundary was passed without a tick occurring (e.g. no tick was invoked for an extended period), the next tick SHALL run that job once to catch up. It SHALL NOT run the job once for every boundary that was missed.

#### Scenario: Single catch-up run after a long gap
- **WHEN** an hourly job's last run was 18 hours ago and a tick finally occurs
- **THEN** the job SHALL run exactly once on this tick, not 18 times

### Requirement: Persisted Last-Run State
The system SHALL persist, in a local JSON state file, the last run time and outcome of each job. This state SHALL be read on every tick to determine due-ness and SHALL be updated after each job execution (whether triggered by tick or run on demand).

#### Scenario: State file updated after execution
- **WHEN** a job finishes executing, successfully or not
- **THEN** the state file SHALL reflect that job's most recent run time and outcome before the process exits

#### Scenario: Due-ness computed from persisted state
- **WHEN** a tick occurs after the process was previously restarted or invoked from a different process
- **THEN** due-ness for each job SHALL be computed using the last-run time recorded in the state file, not from any in-memory state
