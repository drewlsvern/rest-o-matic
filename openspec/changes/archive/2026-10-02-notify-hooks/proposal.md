## Why

A failed backup is silent unless that job has its own `failure` hook, and every new job has to remember to add one. On hosts where cron output goes nowhere, which is most of them, a job can fail for weeks without anyone knowing. A notification command should be written once and apply to every job.

Job hooks are also the wrong shape for notifications. They run on every run, so an hourly job that fails all day sends 24 alerts, and there is no way to say "tell me when it's working again".

## What Changes

- A new top-level `notify` block holds commands that apply to **every** job, with no per-job setup and no opt-out:
  - `failure` runs when a job starts failing, and then at most once every 24 hours for as long as it keeps failing.
  - `recovery` runs once, on the first successful run after a failure.
  - `success` runs after every successful run, for things like healthcheck pings.
- Notification commands get the same variables as a job's `success` and `failure` hooks (`RESTOMATIC_JOB`, `RESTOMATIC_OUTCOME`, `RESTOMATIC_FAILED_REPOS`, `RESTOMATIC_ERROR`), plus `RESTOMATIC_FAILING_SINCE`.
- They run after the job's own hooks, and never change the job's outcome. A notification command that itself fails is reported as a warning.
- A `failure` notification that could not be sent (its command failed) is tried again on the job's next failed run, instead of waiting out the 24 hours.
- The state file records since when each job has been failing, and `status` shows it.
- A job's own `hooks` are unchanged: they still run on every run.

## Capabilities

### New Capabilities
- `job-notifications`: when the `failure`, `recovery` and `success` notification commands run, what they are given, and how repeated failures are limited.

### Modified Capabilities
- `config`: a new top-level `notify` block and its accepted shape.
- `job-status`: `status` shows since when a failing job has been failing.

## Impact

- `internal/config`: a `Notify` type on `Config`, with the same strict key checking as a job's `after` map.
- `internal/execution`: `RunJob` runs the notification commands after the job's outcome hooks, given the job's prior failing state.
- `internal/state`: two new fields per job, `failing_since` and `failure_notified`.
- `cmd/rest-o-matic`: `run` and `tick` pass the prior state in and record the new one; `status` shows `failing_since`.
- README and `rest-o-matic.example.yaml`: a "Notifications" section with ntfy and healthcheck examples.
- No change for configs without a `notify` block. No new dependencies.

Not included: a per-job opt-out, a configurable repeat interval, locked values inside notification commands, and notifications for things a job hook can't see (cron not firing, an invalid config, a host that is down).
