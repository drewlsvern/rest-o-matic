## Context

- `ResticRunner.Backup` collects restic's standard output in a `bytes.Buffer` and reads it after restic exits, keeping only the `summary` line for the snapshot ID ([restic.go](../../../internal/execution/restic.go)). Errors come from standard error and are handled separately (`resticFailure`, `unreadableHint`).
- That buffer holds every `status` line. restic writes about 10 a second with `--json` (restic 0.19.1; 0.16.4 also sends them), each about 300 bytes. A day-long backup therefore holds several hundred MB of status lines in rest-o-matic's memory just to read the last line. Reading the output as it arrives fixes this as a side effect.
- restic's status line has `percent_done`, `total_files`, `files_done`, `total_bytes`, `bytes_done`, `seconds_elapsed`, `seconds_remaining` (missing until restic can estimate) and `current_files` (missing in some lines). Both versions use the same fields. The totals grow while restic is still scanning, so `percent_done` can fall back early in a run.
- `RunJob` runs the before hooks, then `runRepo` (backup, then forget) for each repository, then the after hooks ([job.go](../../../internal/execution/job.go)). Hook output is captured into a buffer, not written to the terminal.
- The state file is written under a cross-process lock that doesn't wait: a second writer fails with "state is locked by another process" ([state.go](../../../internal/state/state.go)). Per-job snapshot lists already live beside it in `snapshots/<job>.json`.
- `status` decides whether a job is running from whether its job lock is held. The check-in's *status* part is built from the same job entries `status --json` prints.
- Colour is decided per stream from `--color`, `NO_COLOR` and `term.IsTerminal`. With colour disabled, no escape sequence may be written, even on a terminal.

## Goals / Non-Goals

**Goals:**
- `status` answers "where is it, how far, is it still moving" for any running job, however it was started.
- A person running `run` sees progress live, and a log of `run` or `tick` keeps a readable record.
- No new way for a job to fail, and no new contention on the state file.

**Non-Goals:**
- Acting on a pause: no warning, notification, timeout or kill.
- Progress in the check-in or the central app.
- Progress for `forget`. It is usually quick and restic reports no progress for it; its phase is shown.

## Decisions

### Reading restic's output as it arrives

`Backup` gets an `io.Writer` for standard output that splits lines and decodes each one. A `status` line goes to a callback. The `summary` line is kept for the snapshot ID. Anything else is ignored, as now. Lines can be long, because `current_files` holds full paths, so the splitter has no small line limit. Standard error is still collected whole: it is small, and the error handling reads it after exit, as now.

The callback is passed through `Options`, so `Backup` doesn't know where progress goes. A nil callback costs only the decoding.

### Where progress is recorded: one file per job

`<state-dir>/progress/<job>.json`, written by writing a temporary file and renaming it over the old one, **without** the state lock. Only the process holding the job's lock writes it, so there is never a second writer. Readers see either the old file or the new one, because of the rename. Putting it in `state.json` was rejected, because a write every 5 seconds would make other jobs' `RecordRun` hit the state lock and fail.

```json
{
  "phase": "backup",
  "repository": "s3", "repository_index": 1, "repositories": 2,
  "percent_done": 0.43, "bytes_done": 3650000000, "total_bytes": 8500000000,
  "files_done": 120345, "total_files": 210000, "seconds_remaining": 5400,
  "current_files": ["/home/me/.config/Code/…"],
  "updated": "2026-10-21T09:14:05Z",
  "last_report": "2026-10-21T09:14:04Z",
  "last_moved": "2026-10-21T09:10:51Z"
}
```

Phases are `before_hooks`, `backup`, `forget` and `after_hooks`. `last_report` is when restic last sent a status line. `last_moved` is when `bytes_done` or `files_done` last went up. The two together tell "waiting on the upload" (restic still reports, figures still) from "restic has gone quiet" (no report at all).

A phase change is written immediately. restic's figures are written at most every 5 seconds: the recorder keeps the latest line in memory and writes when 5 seconds have passed since its last write. The file is removed in the same deferred cleanup that clears the running marker. A write error is reported once, as a warning, and the job carries on.

### Showing it in `status`

`status` reads `progress/<job>.json` only for a job whose lock is held, the same rule as "running since". It never writes or removes the file. Default output:

```
config   running for 1h10m (run): backing up to s3 (1 of 2), 43%, 3.4 of 7.9 GiB, about 1 hour left
config   running for 1h10m (run): backing up to s3 (1 of 2), 43%, 3.4 of 7.9 GiB, no progress reported for 12 minutes
mssql    running for 2m (tick): before hooks
```

"No progress reported for N" replaces the time left once `last_moved` is more than a minute old. A minute avoids flicker between packs, and "no progress reported" doesn't claim there's a fault. `status <job>` adds the files being read, `last_report` and `last_moved`. In `--json`, `running` gains a `progress` object with the fields above as RFC 3339 UTC times, left out when the job has recorded none.

The check-in builds its job entries without progress, so `progress` is left out and the *status* part stays byte-for-byte as it is today and a running job doesn't change it on every tick. A later change to the check-in contract can carry progress on purpose.

### Progress on the command line

One printer per `run` or `tick` process, writing to standard error behind a mutex, fed by every job running in that process. Standard output, where the result lines go, is untouched.

- **Terminal** (`term.IsTerminal` on standard error): a single line, redrawn at most 4 times a second, joining the running jobs with ` | `. It is cut to the terminal's width (`term.GetSize`, 80 if unknown) so it never wraps. It is redrawn with `\r` followed by the new text, padded with spaces to cover the old one. No `ESC[K`, so it holds with `--color=never`. Before rest-o-matic writes anything else to either stream (a job's result, a warning), the printer clears the line. Those writes go through the printer's `Clear`.
- **Not a terminal**: complete lines only, each starting with the local time in RFC 3339 and the job's name, ASCII only:
  ```
  2026-10-21T09:14:05-05:00 config: backing up to s3 (1 of 2)
  2026-10-21T09:19:05-05:00 config: backing up to s3 (1 of 2), 12%, 1.0 of 7.9 GiB, about 5h left
  ```
  One line at each phase change, and one per job in a backup every `progress_interval`.

The printer takes its updates from the same callbacks as the recorder, not by reading files back.

Considered: writing progress to standard output. It would mix with the result lines that scripts read, and put progress into `run config > result.txt`.

### `progress_interval`

A top-level key next to `max_concurrent`, because one process prints for every job it runs. It's a duration string (`30s`, `5m`, `1h`), or `0` for timed lines off, defaulting to `5m`. The minimum is `10s`, so a log can't be flooded by mistake. It gets its own type with an `UnmarshalYAML`, so a bad value is reported at its line like other type errors. The schema allows a string matching `^[0-9]+(s|m|h)$` or the integer `0`. The 5-second recording interval is fixed and not part of the config. It only bounds how stale `status` can be.

## Risks / Trade-offs

- [Every 5 seconds is a file write, for a whole day-long run] → It's a few hundred bytes renamed in place, about 17,000 small writes a day. That's acceptable on SSDs and network home directories, and it avoids the state lock entirely.
- [`percent_done` falls back early on while restic is still counting files] → Show restic's figures as given. It settles once the scan finishes, and showing exactly what restic reports is easier to trust than a smoothed number.
- [A pause while uploading reads as "no progress"] → That's why the wording doesn't say "stalled", and nothing acts on it. `last_report` shows restic is still alive.
- [Output from a cron job that mails its output gets longer] → It goes to standard error only, about 300 lines for a day-long run at the default. `progress_interval: 0` leaves only the phase lines.
- [The terminal line overwrites a line written by something else, such as a hook or restic, that bypasses the printer] → Hooks and restic don't write to the terminal (their output is captured), so only rest-o-matic's own messages need to go through `Clear`.

## Migration Plan

No migration. Without a `progress/` directory, `status` shows what it shows today. An older `status` ignores the directory. `run` and `tick` gain output on standard error only.
