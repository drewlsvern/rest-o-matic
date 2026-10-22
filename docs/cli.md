# Command reference

## Commands

| Command | What it does |
|---|---|
| `rest-o-matic validate [--json]` | Checks the config file and reports every problem with its line, without running anything; `--json` prints one JSON document |
| `rest-o-matic schema` | Prints a JSON Schema of the config file, for editors |
| `rest-o-matic run <job>` | Runs one job now, whether or not it is due, showing its [progress](#progress-while-a-job-runs) |
| `rest-o-matic tick` | Runs every job that is due, then exits. This is what your scheduler calls |
| `rest-o-matic status [job]` | Shows each job's last run, outcome and next due time, and how far a running job has got; with a job name, its recent runs |
| `rest-o-matic exec <repository> -- <restic args>` | Runs a restic command against a configured repository |
| `rest-o-matic secret <keygen\|public-key\|lock\|reveal\|check\|relock>` | Manages locked config values |
| `rest-o-matic enrol <url>` | Connects this host to the [central app](central-app.md); `enroll` works too |
| `rest-o-matic unenrol` | Disconnects it again, keeping the host key |
| `rest-o-matic checkin [--print]` | Reports to the central app now, or prints what it would send |
| `rest-o-matic completion <shell>` | Prints a shell completion script |
| `rest-o-matic --version` | Prints the version and build details |

Every command has `--help`.

## Global flags

| Flag | Default | Meaning |
|---|---|---|
| `--config` | `rest-o-matic.yaml` in the working directory | The config file |
| `--state-dir` | `.rest-o-matic` in the working directory | Where state and lock files are kept |
| `--key-file` | `host.key` in your user config directory | The host key for locked config values |
| `--color` | `auto` | `auto`, `always` or `never` |

For anything scheduled, give `--state-dir` as an absolute path.

## Exit status

| Command | Non-zero when |
|---|---|
| `validate` | The config has an error, or couldn't be read. Warnings alone exit 0. The same with `--json` |
| `run` | The job failed, was interrupted, or is already running |
| `tick` | A due job failed or could not be started. A job skipped because it is already running does not count |
| `status` | It could not report (invalid config, unreadable state). The health of the jobs does not affect it |
| `exec` | restic's own exit code, or 20 to 23 when rest-o-matic refused to run it (see [Running restic commands](restic-commands.md#exit-codes)) |
| `secret check` | A locked value cannot be unlocked on this host |
| `enrol` | Enrolment failed, or the host is already enrolled and `--force` wasn't given. Not adding an offered recovery key is not a failure |
| `checkin` | The check-in failed, or the host isn't enrolled for this config. A failed check-in never changes `tick`'s exit status |
| `secret relock` | A locked value cannot be opened, or there is no host key. The config is then left unchanged |

## Output colours

When run in a terminal, rest-o-matic colours its own status labels: errors
red, warnings orange, successes green. Only the label is coloured, and restic's
own output is never touched. Colour is decided separately for stdout and
stderr, so it's on only for a stream that's a terminal. Output going to cron
mail, the systemd journal, a pipe or a file stays exactly as plain as before.

- `--color=auto` (default): colour only on a terminal
- `--color=always` / `--color=never`: force it on or off
- `NO_COLOR=1`: turn it off, unless `--color=always` is given

## Progress while a job runs

`run` and `tick` report each job's progress as it runs, on **standard
error**. Standard output keeps only the results (`job config: OK` and the
lines under it), so a script that reads them is unaffected.

On a terminal, one line is redrawn in place:

```
config: backing up to s3 (1 of 2), 43%, 3.4 GiB of 7.9 GiB, 12.4 MiB/s, about 1 hour left
```

It says what [`status`](status.md#a-running-jobs-progress) says, including
`restic is retrying: …` while restic retries a connection.

When standard error goes to a file or a pipe, complete lines are written
instead: one each time a job moves to another phase, one for each retry
message restic prints, and one for each job in a backup every [`progress_interval`](configuration.md#progress-lines)
(5 minutes by default):

```
2026-10-21T09:14:05-05:00 config: backing up to s3 (1 of 2)
2026-10-21T09:19:05-05:00 config: backing up to s3 (1 of 2), 12%, 1.0 GiB of 7.9 GiB, about 5 hours left
2026-10-21T13:02:41-05:00 config: applying retention on s3 (1 of 2)
```

The shell chooses where each stream goes:

| You want | Command |
|---|---|
| Results and progress in one log | `rest-o-matic run config > run.log 2>&1` |
| The same, from cron | `rest-o-matic tick >> tick.log 2>&1` |
| To watch it and keep a copy | `rest-o-matic run config 2>&1 \| tee run.log` (bash and zsh: `\|& tee run.log`) |
| Results in a file, live progress on screen | `rest-o-matic run config > result.txt` |
| Progress only, in its own file | `rest-o-matic run config 2> progress.log` |

`2>&1` sends standard error wherever standard output is going at that
point, so put it after `> run.log`. Under systemd, both streams go to the
journal anyway.

Progress lines contain no colour or other escape sequences, whatever
`--color` is.
