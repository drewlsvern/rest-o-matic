## Why

When a job is running, `status` says only "running since 1h10m (started by run)". Nothing shows whether the job is in a hook, which repository it is backing up to, how far it has got, or whether it has stopped moving. A backup that will take a day over a slow upload looks the same as one stuck on an unreachable server. Today the only way to tell is to read `/proc/<pid>/io` and the sockets of the restic process. `run` is silent until the job finishes, and so is `tick`.

restic already reports all of this. With `--json` it writes a status line about ten times a second (checked with restic 0.19.1 and 0.16.4), with percent done, estimated time left, bytes and files done and in total, and the files being read. rest-o-matic holds restic's output until it exits, then keeps only the summary line.

## What Changes

- **restic's output is read as it arrives**, not held until it exits. The snapshot ID and error handling are unchanged.
- **A running job records its progress** in its own file in the state directory, at most every 5 seconds. The file holds the phase (before hooks, backup to repository *i* of *n*, retention, after hooks), restic's latest figures, when restic last reported, and when its figures last moved. It is removed when the job ends.
- **`status` shows a running job's progress** next to "running since", while the job's lock is held, and **`status <job>` and `status --json`** add the files being read and how long ago the figures last moved. A pause is reported as "no progress reported for N", and nothing is done about it: no warning, no notification, no timeout. On a slow upload restic stops reading for minutes at a time while it sends data, so a pause on its own isn't a fault.
- **`run` and `tick` report progress as they go**, on standard error. On a terminal, one line is redrawn in place. Otherwise, a plain line is written per job at a set interval and at every change of phase, so a log keeps a readable record.
- **New top-level config key `progress_interval`** (default `5m`): the interval for those plain lines. `0` turns the timed lines off and keeps the phase lines.
- **The check-in does not carry progress.** Showing progress in the central app is left for a later change to the check-in contract. Until then, the status part sent to the central app stays as it is, so a running job doesn't make it change on every tick.

## Capabilities

### New Capabilities

None.

### Modified Capabilities
- `backup-execution`: a running job records its phase and restic's progress.
- `job-status`: running jobs show their progress.
- `cli-output`: `run` and `tick` print progress to standard error; the requirement that redirected output stays unchanged is narrowed to colour, which is what it was written for.
- `config`: the `progress_interval` key.
- `checkin`: the status part leaves out a running job's progress.

## Impact

- `internal/execution`: `ResticRunner.Backup` reads restic's standard output line by line; `RunJob` reports phases; a progress recorder, throttled to 5 seconds.
- `internal/state`: a `progress/<job>.json` file per job, written atomically and without the state lock, read by `status`.
- `cmd/rest-o-matic`: the progress display in `status`, `status <job>` and `status --json`; progress output for `run` and `tick`; the check-in's status part leaves progress out.
- `internal/config`: `progress_interval`, with the schema and validation.
- Docs: `docs/cli.md` (status, run, tick), `docs/configuration.md` (`progress_interval`), the example config.
- Output: `run` and `tick` now write progress to standard error. Their standard output is unchanged, so anything reading the result lines keeps working. A cron job that mails its output would now include progress lines on standard error; `progress_interval: 0` reduces them to one line per phase.
