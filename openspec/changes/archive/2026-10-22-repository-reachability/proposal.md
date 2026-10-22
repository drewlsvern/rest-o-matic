## Why

On 2026-10-21 the iDrive e2 endpoint behind the user's `s3` repository stopped accepting connections. `run config` sat at `backing up to s3 (1 of 2)` with nothing else to see: restic was retrying the connection, but rest-o-matic only reads restic's standard error once restic exits, so its messages (`dial tcp 192.154.138.10:443: i/o timeout, retrying`) never reached `status`. Measured against an endpoint that doesn't answer, restic 0.19.1 keeps retrying for about 14 minutes before it gives up (restic 0.16.4 gives up after the first timeout). Every job using that repository spends those 14 minutes, every run, before it moves on to its next repository.

Two days earlier the same endpoint had been transferring at about 85 KiB/s on a 500 Mbit/s line. Nothing showed that either, short of reading `/proc`.

## What Changes

- **Connection check before each repository's backup.** A quick restic call that opens the repository, with a time limit: `connect_timeout`, default `2m`. It applies to every repository, local ones included, since a "local" path can be a network share mounted on demand (the user's `ut-g5` is CIFS behind autofs). If the check runs out of time, that repository's result is **`unreachable`**, its backup is skipped, and the job moves on. If restic fails within the time limit, for example with a wrong password or a missing repository, the result is `backup_failed` with restic's message, as today.
- **`connect_timeout`** on a repository, and at the top level as the default for all of them. It's a duration (`30s`, `2m`, `1h`) or `0` to skip the check.
- **`unreachable`, a new repository result.** It appears in run records, `status`, `status --json` and the check-in. Its error is the latest of restic's own retry messages, or restic's final message when there is one. Hooks and notifications get a new `RESTOMATIC_UNREACHABLE_REPOS`. `RESTOMATIC_FAILED_REPOS` still lists every failed repository, unreachable ones included.
- **restic's retry messages are shown live.** restic's standard error is read as it arrives. The latest retry message goes into the job's progress record, and `status`, the terminal line and progress logs show `restic is retrying: <message>` until restic reports progress again. restic 0.16 doesn't print these, so nothing new is shown with it.
- **"no report from restic yet (N minutes)"** in `status` and progress lines, when a backup has gone more than a minute without any report from restic.
- **Throughput** next to the percentage: restic's bytes done divided by the time the backup has run (`12.4 MiB/s`).
- **`FAILED (1 of 2 ok)`** in `status` when some repositories of a failed run succeeded. This is display only: the recorded outcome stays `failed`.
- **Not in this change:** retrying a failed job before its next scheduled run. That changes when jobs run and needs its own decisions (how soon, which failures, hooks), so it comes after this change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities
- `config`: the `connect_timeout` key, on repositories and at the top level.
- `backup-execution`: the connection check, the `unreachable` result, restic's retry messages and the check phase in the progress record, and `RESTOMATIC_UNREACHABLE_REPOS` for hooks.
- `scheduling`: run records can hold the `unreachable` result.
- `job-notifications`: `RESTOMATIC_UNREACHABLE_REPOS`.
- `job-status`: unreachable repositories, `FAILED (i of n ok)`, retry messages, "no report from restic yet", and throughput.
- `cli-output`: progress lines and the terminal line show the check phase, retry messages and throughput.

## Impact

- `internal/execution`:
  - a `Check` call on `ResticRunner` (`restic cat config --no-lock` with a deadline, through the job's read mode);
  - `runRepo` runs it first;
  - restic's standard error is read line by line for retry messages, keeping the whole of it for error reporting, as now;
  - a `check` phase;
  - `RepoOutcome` can be unreachable.
- `internal/state`: `ResultUnreachable`; the progress record gains the phase start time, seconds elapsed and the latest retry message.
- `internal/config`: `connect_timeout` (the duration type `progress_interval` uses), the schema, validation.
- `cmd/rest-o-matic`:
  - the `unreachable` line and `FAILED (i of n ok)` in `status`;
  - retry messages, "no report yet" and throughput in `status` and the printer;
  - the result in the run record.
- `contract/checkin/v1`: `unreachable` added to the repository result's description. It's an added value, not a format change: the contract already says each side ignores what it doesn't know. Note it in the UI repo handoff.
- Docs: `docs/configuration.md` (`connect_timeout`), `docs/status.md`, `docs/notifications.md`, `docs/cli.md`.
- Behaviour: each repository costs one extra restic call per run. That takes well under a second when the repository is reachable (about 0.5 s for a local repository, measured). A job whose repository can't be reached now fails that repository after 2 minutes instead of about 14.
