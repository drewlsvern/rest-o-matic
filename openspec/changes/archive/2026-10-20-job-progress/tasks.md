## 1. Reading restic's output

- [x] 1.1 `Backup` takes a progress callback (through `Options`) and reads restic's standard output line by line: `status` lines go to the callback, the `summary` line gives the snapshot ID, other lines are ignored; no small line-length limit; standard error and error handling unchanged
- [x] 1.2 Tests with the fake restic: status lines reach the callback in order; the snapshot ID still comes from the summary; a very long `current_files` line is handled; a nil callback works; errors and the unreadable-files hint are unchanged

## 2. Phases and the progress record

- [x] 2.1 `RunJob` reports phase changes (`before_hooks`, `backup` with repository and position, `forget`, `after_hooks`) through the same `Options` hook
- [x] 2.2 `internal/state`: `progress/<job>.json` written atomically without the state lock, read and removed; a missing or unreadable file reads as no progress
- [x] 2.3 A recorder that writes phase changes immediately and restic's figures at most every 5 seconds, tracking `last_report` and `last_moved`; reports a write error once as a warning and carries on
- [x] 2.4 `run` and `tick` give each job a recorder, and remove the file in the cleanup that clears the running marker, on every way out (success, failure, interrupt)
- [x] 2.5 Tests: phase sequence for a job with hooks and two repositories; throttling with a fake clock; `last_moved` holds while figures stand still; file removed after success, failure and interrupt; a `podman-unshare` job records progress; a write failure doesn't fail the job

## 3. Status

- [x] 3.1 `status` reads the progress of jobs whose lock is held; default output adds phase, repository (i of n), percent, bytes and time left, or "no progress reported for N" after a minute without movement
- [x] 3.2 `status <job>` adds the files being read, last report and last movement; `status --json` gains `running.progress` (an object, left out when there is none)
- [x] 3.3 The check-in builds its status part without progress
- [x] 3.4 Tests: running job with progress (text and JSON); paused job; job in a hook; leftover file with no lock held is ignored; `status` leaves the file untouched; a check-in's status fingerprint is the same before and after progress moves

## 4. Command-line progress

- [x] 4.1 A printer per process on standard error: terminal mode (one line, at most 4 redraws a second, ` | `-joined jobs, cut to the terminal width, `\r` plus space padding, no escape sequences) and plain mode (timestamped complete lines at phase changes and every `progress_interval`)
- [x] 4.2 Route `run`'s and `tick`'s own result lines and warnings through the printer's `Clear`, so the terminal line is never left inside other output
- [x] 4.3 Tests: plain mode with a fake clock writes phase lines and timed lines at the interval, none when it's `0`; lines for two jobs never mix within a line; terminal mode never writes `\x1b` and fits the given width; standard output of `run` is unchanged

## 5. Config

- [x] 5.1 `progress_interval`: a duration type with `UnmarshalYAML` (`Ns`/`Nm`/`Nh` or `0`, minimum `10s`, default `5m`), errors located at its line; schema entry; added to the schema reflection test
- [x] 5.2 Tests: default, `1m`, `0`, `often` and `2s` rejected with the key named

## 6. Docs and Verification

- [x] 6.1 `docs/cli.md`: progress in `status`, `status <job>` and `--json`; progress output from `run` and `tick` on standard error, terminal and plain modes
- [x] 6.2 `docs/configuration.md` and `rest-o-matic.example.yaml`: `progress_interval`
- [x] 6.3 Run a real backup of a large folder: watch `status` while it runs, and check the `run` terminal line and a redirected log by hand; run the progress tests with restic 0.16.4 as well
- [ ] 6.4 `go test ./...` and `go vet ./...` pass locally, and CI (including the Windows job) is green
