## 1. Config

- [x] 1.1 Add `Notify{Failure, Recovery, Success []string}` to `Config` under `notify`, with an `UnmarshalYAML` that accepts only a map of those three keys and rejects any other key or shape with its line
- [x] 1.2 Add `notify` to the top-level sections walked for misplaced `!locked` values
- [x] 1.3 Tests: all three keys; only some; an unknown key names the key and line; a list or string in place of the map is rejected; a string in place of a list is rejected; no block leaves all three empty; `!locked` inside `notify` is rejected

## 2. State

- [x] 2.1 Add `failing_since` and `failure_notified` to `JobState`, both optional
- [x] 2.2 Extend `RecordRun` to take them, writing them with the run record in one update and clearing both on a successful run
- [x] 2.3 Tests: set on failure, kept across further failures, cleared on success; an earlier-format file with `last_outcome: failed` loads with neither field

## 3. Running Notifications

- [x] 3.1 `execution`: add the job's prior state (previous outcome, failing since, failure notified) and a clock to `Options`, and `NotifyErrs`, `FailingSince` and `FailureNotified` to `JobResult`
- [x] 3.2 `RunJob`: after the job's outcome hooks, work out which of `failure`, `recovery` and `success` apply from the table in design.md, run them with `runHooksAll` under the after-hook context, with `recovery` before `success`
- [x] 3.3 Pass the outcome variables plus `RESTOMATIC_FAILING_SINCE` (UTC, RFC 3339; empty for `success`)
- [x] 3.4 Treat the failure notification as sent only when every `failure` command exited zero
- [x] 3.5 `executeAndRecord`: read the job's prior state before the run, pass it in, and record the new failing state with the run; `printResult` reports notification command failures as warnings
- [x] 3.6 Tests with a fixed clock: first failure notifies; a second failure an hour later doesn't; one 25 hours later does; a failed notification command is retried on the next failure; two jobs are limited separately; recovery runs once and not on continued success; success runs every time; a first ever success runs `success` only
- [x] 3.7 Tests: the variables each kind receives, including failing-since staying at the first failure; order after the job's own hooks; a failing notification command doesn't change the outcome; an interrupted run notifies as a failure within the cleanup limit; a job that was already failing in an earlier-format state notifies and later recovers
- [x] 3.8 Command tests: a skipped (already running) job runs no notification; `tick` and `run` both notify

## 4. Status

- [x] 4.1 Add `failing_since` to the JSON report (null when not failing) and a `failing since` line under a failed job in the table when it has failed more than once in a row
- [x] 4.2 Tests for both, with a fixed clock

## 5. Docs and Verification

- [x] 5.1 README "Notifications" section: the three kinds, the once-a-day limit, the variables, ntfy and healthcheck examples, and what notifications can't see
- [x] 5.2 `rest-o-matic.example.yaml`: a commented `notify` block
- [x] 5.3 Manual check with a local listener standing in for ntfy: a job made to fail three times sends one alert, then one recovery when fixed
- [x] 5.4 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` pass; `openspec validate notify-hooks --strict` passes
