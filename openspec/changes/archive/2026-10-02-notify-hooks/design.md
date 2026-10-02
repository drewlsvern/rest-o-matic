## Context

- A job's hooks are `before`, and `after` split into `always`, `success` and `failure` ([types.go](../../../internal/config/types.go)). They run inside `RunJob`, which also applies the time limit on hooks once a run is interrupted ([job.go](../../../internal/execution/job.go)).
- `RunJob` knows nothing about earlier runs. The state file is read and written by the caller, `executeAndRecord` ([exec.go](../../../cmd/rest-o-matic/exec.go)), which holds the job's lock for the whole run, so a job's recorded state can't change underneath it.
- Since `job-status-reporting`, the state file holds each job's last outcome and recent runs, and all writes go through one locked update.
- A config can already share a hook between jobs with a YAML anchor, but each job must reference it, and an anchor replaces a list, so a job can't have both its own `failure` hook and the shared one.

See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- One place to say "tell me when any job fails".
- A job that keeps failing produces one alert a day, not one per run, and one when it recovers.
- Nothing changes for a config that doesn't use it.

**Non-Goals:**
- Reporting what a hook can never see: cron not firing, a config that fails validation, a host that is down. Those need something outside the host, which is the central app's job.
- Global `before` or `always` hooks. Nobody has a use for them yet, and a failing one would fail every job.
- A per-job opt-out, and a configurable repeat interval. Both can be added later without changing what is here.
- Locked values inside notification commands. A token in a command is plain text in the config, as it is in any hook today.

## Decisions

### A `notify` block, not a top-level `hooks` block
```yaml
notify:
  failure:  ["curl -fsS -d \"$RESTOMATIC_JOB failed: $RESTOMATIC_ERROR\" https://ntfy.example.com/backups"]
  recovery: ["curl -fsS -d \"$RESTOMATIC_JOB is working again\" https://ntfy.example.com/backups"]
  success:  ["curl -fsS https://hc-ping.com/<ping-key>/$RESTOMATIC_JOB"]
```
The first idea was a top-level `hooks` block with the same shape as a job's. It was dropped because these commands don't behave like a job's hooks: `failure` is limited to once a day, and `recovery` has no job-level equivalent. Two things called `failure` hooks with different rules would be a permanent source of confusion. A different name makes the difference plain: a job's hooks are actions tied to the run (stop a container, dump a database); `notify` commands tell someone about a change.

It also leaves a top-level `hooks` block free, should global `before`/`always` hooks ever be wanted.

### Three kinds, because one job can't do all three
| Kind | Runs | For |
|---|---|---|
| `failure` | On the first failure, then at most every 24 hours while still failing | Alerts |
| `recovery` | On the first success after a failure | "It's fixed" |
| `success` | On every success | Healthcheck pings, which must arrive every time or the check goes red |

`success` is kept separate from `recovery` for that last reason. Limiting `success` to recoveries would make healthcheck-style monitoring impossible from here.

### "Failing" means the last recorded outcome was failed
The decision uses the job's `last_outcome` from before this run, not the presence of the new fields. That makes a job that was already failing when this version was installed behave sensibly: its next failure notifies, and its next success is a recovery.

| Previous outcome | This run | `failure` | `recovery` | `success` |
|---|---|---|---|---|
| none or success | failed | runs | | |
| failed | failed | runs if never sent, or sent 24 hours or more ago | | |
| failed | success | | runs | runs |
| none or success | success | | | runs |

### Two new fields per job in the state file
`failing_since` is set on the first failure of a run of failures and cleared on success. `failure_notified` is the last time the `failure` commands all exited zero, and is cleared on success.

`failure_notified` is only set when every `failure` command succeeded. If ntfy is unreachable the alert wasn't delivered, so the next failed run tries again instead of staying quiet for a day. `recovery` is attempted once; if it fails that is reported, but there is no later run that would be "the recovery" to retry on.

The 24 hours are measured from `failure_notified`, per job, on a rolling basis.

### `RunJob` runs them; the caller supplies and records the state
The notification commands must run inside `RunJob`, after the outcome hooks, because that is where the after-hook context and its time limit live. So:

- `executeAndRecord` reads the job's prior state (it already holds the job lock) and passes `RunJob` what it needs: the previous outcome, `failing_since` and `failure_notified`.
- `RunJob` decides which lists apply, runs them, and returns what happened in the `JobResult`: any command failures, and whether the `failure` notification was sent.
- `executeAndRecord` writes the new `failing_since` and `failure_notified` with the run record, in the same state update.

`RunJob` stays free of any state-file access, and the clock it uses for the 24-hour check is passed in so tests can control it.

### Strict keys, like a job's `after` map
`notify` gets a custom YAML unmarshaller that rejects any key other than `failure`, `recovery` and `success` with the key's line, so a misspelt key can't mean "never notified". `notify` also joins the top-level sections checked for misplaced `!locked` values.

### In `status`
The JSON gains `failing_since` on each job (null when not failing). It is an added key, so `format_version` stays at 1. In the table, a failed job gets one more line beneath it when it has failed more than once in a row: `failing since <when>`.

## Risks / Trade-offs

- **[Risk] A notification command hangs.** → It runs under the same context as the job's after hooks, so an interrupt bounds it. An ordinary run has no hook timeout today, for any hook; that gap is unchanged.
- **[Risk] A token in a notification command is plain text in the config.** → Same as any hook. Noted as a non-goal; lockable variables for hooks would be the follow-up.
- **[Trade-off] No opt-out.** A job that fails by design would alert daily. → Accepted for now; none exists.
- **[Trade-off] A failure that is fixed and breaks again within 24 hours notifies twice,** once for each run of failures. → Intended: each is a new event, with a recovery between them.
- **[Trade-off] The limit is a fixed 24 hours.** → Simple to explain. A setting can be added if someone needs another interval.

## Migration Plan

Nothing to do. Without a `notify` block nothing changes. An older binary ignores the two new state fields and drops them when it next writes, which only resets the once-a-day limit.
