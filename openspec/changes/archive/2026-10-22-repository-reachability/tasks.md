## 1. Config

- [x] 1.1 `Interval` parse errors name the key they are for; `progress_interval`'s messages are unchanged
- [x] 1.2 `connect_timeout` on `Repository` and at the top level (`*Interval`), with `Config.ConnectTimeoutFor(repo)` resolving repository, then top level, then 2m; schema entries; schema reflection test updated; `configSections` gains the top-level key
- [x] 1.3 Tests: default 2m; top-level default; repository overrides top level; `0` off; `soon` and `5s` rejected at their line naming `connect_timeout` and the repository

## 2. Reading restic's standard error

- [x] 2.1 Generalise the line splitter to call a function per line; standard error keeps its whole text for `resticFailure`/`unreadableHint` and passes retry lines (matched as in the design) to a callback, in `Check` and `Backup`
- [x] 2.2 `Progress` gains `Retry(job, message)`; `Progresses`, the recorder (immediate write, `last_retry`, `last_retry_at`) and the printer implement it
- [x] 2.3 Tests: the 0.19.1 retry line yields the message without the bookkeeping; other stderr lines are not retries; restic's errors are reported exactly as before; the recorder writes a retry at once

## 3. The connection check

- [x] 3.1 `ResticRunner.Check(ctx, repo, mode, timeout, onRetry)`: `cat config --no-lock --json` through the read mode, stopped at the deadline like an interrupted backup; returns ok, unreachable (with the last retry message or a default message), or restic's failure
- [x] 3.2 `runRepo` runs the check first (phase `check`), records `unreachable` or `backup_failed` and skips backup and forget; `RepoOutcome.Unreachable`; `FailureSummary` wording; `RESTOMATIC_UNREACHABLE_REPOS` alongside `RESTOMATIC_FAILED_REPOS`
- [x] 3.3 `state.ResultUnreachable` and the run record's `check_timeout`; `runRecord` maps the outcome
- [x] 3.4 Tests with fake restic: a check that never ends is stopped at a short timeout and gives `unreachable` with the last retry message; the next repository still runs; a fast failure gives `backup_failed`; `connect_timeout: 0` runs no check; a `podman-unshare` job's check goes through the wrapper; the hook and notification environment for an unreachable repository
- [x] 3.5 Integration test against real restic: a healthy local repository passes the check; a missing one gives `backup_failed`; an S3 URL at an address that never answers (192.0.2.1), with a 10s timeout, gives `unreachable` with restic 0.19 and `backup_failed` or `unreachable` with 0.16, depending on how fast its single attempt times out

## 4. Progress additions

- [x] 4.1 The progress record gains `phase_started`, `seconds_elapsed`, `last_retry` and `last_retry_at`; the `check` phase
- [x] 4.2 `progressSummary`: `checking <repo> (i of n)`; throughput once seconds elapsed is at least 5; "no report from restic yet (N)" after a minute of a backup without a report; "restic is retrying: <message>" when the retry is newer than the last report; `status <job>` and JSON show the retry time
- [x] 4.3 Plain progress lines: one per retry message
- [x] 4.4 Tests: each display rule, including a retry that stops showing once restic reports progress; the plain-mode retry lines; JSON fields

## 5. Status for failed runs

- [x] 5.1 `unreachable (checked for 2m): <error>` repository notes; `FAILED (i of n ok)` in the default table when some repositories succeeded
- [x] 5.2 Tests: one unreachable and one ok; one failed and one ok; all failed (no count); before-hook failure (no count); `run`'s result lines and JSON unchanged

## 6. Contract, docs and verification

- [x] 6.1 `contract/checkin/v1`: add `unreachable` to the repository result's description and an example; contract tests pass
- [x] 6.2 Docs: `docs/configuration.md` (`connect_timeout`, why local repositories are checked too), `docs/status.md` (unreachable, the count, retrying, no report yet, throughput and what it measures), `docs/notifications.md` and the hooks docs (`RESTOMATIC_UNREACHABLE_REPOS`), `docs/cli.md` (progress lines), the example config
- [x] 6.3 UI repo handoff: `unreachable` result, `RESTOMATIC_UNREACHABLE_REPOS`, new progress fields (not sent in the check-in)
- [x] 6.4 By hand: a job with an unreachable S3 repository and a local one, watched through `status` and a redirected log; run the new tests with restic 0.16.4
- [ ] 6.5 `go test ./...` and `go vet ./...` (also `GOOS=windows`) pass locally, and CI is green
