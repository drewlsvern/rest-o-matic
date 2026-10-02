## 1. Job Lock Probe

- [x] 1.1 Confirm `fix-job-concurrency` is in the base (`lock.AcquireJob` exists and `executeWithSlot` holds it until state is written); this branch is stacked on `fix/job-concurrency`, which is not yet merged to `main`
- [x] 1.2 `internal/lock`: add `JobHeld(lockDir, jobName)`, which returns false when the lock file doesn't exist, never creates it, and otherwise tests it with a shared lock released at once
- [x] 1.3 `lock.AcquireJob`: retry briefly (about 250 ms) before reporting the job as held, so a `status` probe can't make a starting job look as if it is already running
- [x] 1.4 Unit tests: `JobHeld` is true while held and false after unlock; `JobHeld` on a missing file is false and creates nothing; `AcquireJob` succeeds when a probe releases within the retry window

## 2. Richer State

- [x] 2.1 `internal/state`: add `RunRecord` and `RepoResult` types, and `Runs`, `Running` on `JobState` and `LastTick` on `State`, keeping `last_run` and `last_outcome` unchanged
- [x] 2.2 Replace `Update` with a single method that takes a cross-process lock on `locks/state.lock` (waiting up to a few seconds), loads, applies a function and writes atomically
- [x] 2.3 Add helpers on top of it: record a finished run (prepend, cap at 20, set `last_run` and `last_outcome`, clear `Running`), mark a job running, and record a tick time
- [x] 2.4 Unit tests: history is newest first and capped at 20; a file with only `last_run`/`last_outcome` loads and keeps them; concurrent updates from separate stores on one path all survive
- [x] 2.5 `execution`: keep the snapshot ID in `RepoOutcome` instead of discarding it
- [x] 2.6 Build a `RunRecord` from a `JobResult` (start, finish, trigger, one-line error via the existing `firstFailure`/`oneLine`, per-repository result, error and snapshot ID); unit test each repository result and the before-hook case
- [x] 2.7 `executeAndRecord`: mark the job running at the start, write the run record at the end, pass the trigger (`tick` or `run`) through, and print a warning when state can't be written
- [x] 2.8 `tick`: record the tick time after the config loads and the owner check passes, before any job starts; test that an invalid config records nothing

## 3. Next Due Time

- [x] 3.1 `internal/schedule`: add `Next(sched, lastRun)` returning the start of the following hour, UTC day or UTC week
- [x] 3.2 Unit tests, including that `Due(sched, last, now)` is true exactly when `now` is at or after `Next(sched, last)` for forward-moving time

## 4. Status Command

- [x] 4.1 Build the report struct from config, state and lock probes: per job the schedule, repositories, last run, `due`, `next_due` and `running` (believed only while the job lock is held; lock held with no `running` field means waiting)
- [x] 4.2 `status`: load and validate the config, read state without creating the state directory, skip the owner check, and exit zero whenever a report was produced
- [x] 4.3 Default output: last tick line and the job table from design.md, with relative times, coloured outcome labels and error lines under failed jobs
- [x] 4.4 `status <job>`: that job's runs, newest first; unknown job is an error with a non-zero exit
- [x] 4.5 `--json`: one document with `format_version`, `generated_at`, `last_tick` and `jobs`, RFC 3339 UTC times, `null` for absent values, and `runs` included for a single job
- [x] 4.6 Tests with a fixed clock: never-run job; successful, failed and before-hook-failed runs; hourly job before and after its boundary; running shown while the lock is held and not when only a stale `running` field remains; old-format state file
- [x] 4.7 Tests: `--json` stdout parses as one document when the config has a warning; missing state directory reports all jobs as never run and creates nothing
- [x] 4.8 Register the command and write its help text

## 5. Docs and Verification

- [x] 5.1 README: a "Status" section with example output, `status <job>` and `--json`, and a note that a job still running from an earlier tick is now skipped
- [x] 5.2 Manual check on Linux: run a job, break a repository URL and run again, then confirm `status`, `status <job>` and `status --json` show the success, the failure and its error
- [x] 5.3 Manual check: `status` run as root against a state directory owned by another user reports normally and leaves no root-owned file (done in a rootless Podman user namespace, with root and a separate uid 1000, instead of with `sudo`)
- [x] 5.4 `go test ./...`, `go vet ./...` and `GOOS=windows go build ./...` pass; `openspec validate job-status-reporting --strict` passes
