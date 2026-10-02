# Command reference

## Commands

| Command | What it does |
|---|---|
| `rest-o-matic validate` | Checks the config file and reports every problem, without running anything |
| `rest-o-matic run <job>` | Runs one job now, whether or not it is due |
| `rest-o-matic tick` | Runs every job that is due, then exits. This is what your scheduler calls |
| `rest-o-matic status [job]` | Shows each job's last run, outcome and next due time; with a job name, its recent runs |
| `rest-o-matic exec <repository> -- <restic args>` | Runs a restic command against a configured repository |
| `rest-o-matic secret <keygen\|public-key\|lock\|reveal\|check>` | Manages locked config values |
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
| `validate` | The config has an error. Warnings alone exit 0 |
| `run` | The job failed, was interrupted, or is already running |
| `tick` | A due job failed or could not be started. A job skipped because it is already running does not count |
| `status` | It could not report (invalid config, unreadable state). The health of the jobs does not affect it |
| `exec` | restic's own exit code, or 20 to 23 when rest-o-matic refused to run it (see [Running restic commands](restic-commands.md#exit-codes)) |
| `secret check` | A locked value cannot be unlocked on this host |

## Output colours

When run in a terminal, rest-o-matic colours its own status labels: errors
red, warnings orange, successes green. Only the label is coloured, and restic's
own output is never touched. Colour is decided separately for stdout and
stderr, so it's on only for a stream that's a terminal. Output going to cron
mail, the systemd journal, a pipe or a file stays exactly as plain as before.

- `--color=auto` (default): colour only on a terminal
- `--color=always` / `--color=never`: force it on or off
- `NO_COLOR=1`: turn it off, unless `--color=always` is given
