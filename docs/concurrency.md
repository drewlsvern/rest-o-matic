# Concurrency and the state directory

Three rules govern which jobs run at the same time, all enforced with
ordinary file locks so they hold even across independently invoked
processes (two overlapping ticks, or a tick and a manual `run`):

- Two jobs never back up to the **same repository** at the same time —
  restic's own repository locking makes this a correctness requirement, not
  just a performance one.
- No more than `max_concurrent` jobs run at once, period.
- A job never runs **twice at once**.

A job that can't start yet **waits**. Before anything of it runs, a job
takes every repository it backs up to and a free slot; if one is in use, it
waits and then starts as soon as it's free, in the same invocation. Its
hooks don't run while it waits, so a `before` hook that stops a container
only does so once the backup can actually begin. `tick` and `run` print a
line when a job starts waiting, and may stay running for as long as the job
ahead takes. When several jobs are waiting for the same repository, all of
them run, but the order they go in isn't guaranteed.

A job that is already running, or already waiting, isn't started again. A
`tick` that finds such a job due reports it as `already running, skipped`
without running its hooks and without counting it as a failure, so a
backup that takes longer than your cron interval is fine. `run <job>`
refuses with an error instead.

`exec` doesn't wait: if a repository is in use it refuses at once (see
[Running restic commands](restic-commands.md#safeguards)).

## One user per state directory

The state file and the lock files live in `.rest-o-matic/` (or
`--state-dir`), relative to the directory rest-o-matic is started from, so
give scheduled runs an absolute `--state-dir`. Always run rest-o-matic as
the same user for a given state directory: the files are created on first
use, readable only by their owner, and never removed. One `sudo
rest-o-matic run …` would leave root-owned files behind that every later
run as your usual user can't open.

`run`, `tick` and any `exec` that takes rest-o-matic's own lock check this
first, and refuse to start if the directory or a file in it belongs to
another user. The error lists every path that doesn't match:

- If the **directory itself** belongs to someone else, run rest-o-matic as
  that user, or pass a separate `--state-dir`.
- If only **files inside it** do (left by an earlier run as another user),
  delete them or `chown` them to your user. They hold no backup data, only
  run history and locks. Deleting the state file makes every job due again.

`exec` subcommands that don't take the lock (`snapshots`, `restore` and the
other read-oriented ones, or anything with `--force`) create no files there,
so they aren't checked. `validate` isn't checked either. The check doesn't
apply on Windows.
