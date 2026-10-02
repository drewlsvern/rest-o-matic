## 1. Locks

- [x] 1.1 `internal/lock`: add `AcquireJob(lockDir, jobName)`, a non-blocking exclusive lock on `locks/job-<name>.lock`, returning `ok=false` with a nil error when another execution holds it
- [x] 1.2 `internal/lock`: add `WaitRepository(ctx, lockDir, repoName)`, which retries the existing non-blocking attempt about once a second until it succeeds or `ctx` is cancelled; keep `AcquireRepository` unchanged for `exec`
- [x] 1.3 `internal/lock`: add `WaitSlot(ctx, lockDir, maxConcurrent)`, retrying across all slot files the same way
- [x] 1.4 Unit tests: a second `AcquireJob` reports not ok while held and succeeds after `Unlock`; `WaitRepository` and `WaitSlot` return once the holder unlocks, and return the context's error when cancelled first

## 2. Acquire Before Running

- [x] 2.1 `executeWithSlot`: acquire the job lock (no wait), then every repository lock the job needs in sorted order with duplicates removed (waiting), then a slot (waiting); release all of them only after `executeAndRecord` has written state
- [x] 2.2 Replace the `report` flag with a result kind: ran, not started, already running; a job interrupted while waiting is "not started"
- [x] 2.3 Print one line when a job starts waiting, naming the repository or saying it is waiting for a slot
- [x] 2.4 `internal/execution`: remove locking from `runRepo`, remove `RepoOutcome.Deferred` and its handling in `firstFailure`, `ok` and `printResult`; update the tests that relied on it
- [x] 2.5 `tick`: print `job <name>: already running, skipped`, keep such jobs out of the succeeded and failed counts, add `N already running` to the summary line, and exit zero when nothing else failed or was left unstarted
- [x] 2.6 `run`: return `job %q is already running` (non-zero exit) when the job lock is held
- [x] 2.7 `tick`: after taking a job's lock, reload state and skip the job silently if it is no longer due

## 3. Tests

- [x] 3.1 Two jobs sharing a repository, dispatched together: both run, one after the other, both recorded as successful
- [x] 3.2 A job whose repository lock is held elsewhere: its `before` hook has not run while it waits, and runs once the lock is released
- [x] 3.3 A job with two repositories, the second held elsewhere: no hook and no backup to the first until the second is free
- [x] 3.4 Cancelling the work context while a job waits: no hooks run, the state file is untouched, the result is "not started"
- [x] 3.5 With the job lock held: `tick` runs none of the job's hooks, leaves state untouched and exits zero; with a second due job, that one still runs
- [x] 3.6 With the job lock held: `run` exits non-zero, names the job, and runs no hook
- [x] 3.7 A job listing the same repository twice does not wait on itself
- [x] 3.8 All slots held elsewhere: the job waits and runs when one is released, and is reported by its real outcome
- [x] 3.9 A job that another process records as run after `tick` computed its due list, but before `tick` takes its lock, is not run again

## 4. Docs and Verification

- [x] 4.1 README "Concurrency": describe waiting (a job waits for its repositories and a slot before anything runs), add the third rule (a job never runs twice; an overlapping `tick` skips it, `run` refuses), and note that `exec` still refuses at once
- [x] 4.2 Repeat reproduction 1 from design.md and confirm the hook log shows one stop and one start, and the second tick exits zero
- [x] 4.3 Repeat reproduction 2 and confirm both jobs succeed and the repository holds a snapshot from each
- [x] 4.4 Manual check: kill a running job's process with SIGKILL and confirm the next `tick` starts the job
- [x] 4.5 `go test ./...`, `go vet ./...` and `GOOS=windows go build ./...` pass; `openspec validate fix-job-concurrency --strict` passes
