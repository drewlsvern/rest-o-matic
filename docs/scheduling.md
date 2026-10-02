# Scheduling

rest-o-matic never runs as a daemon and never touches your system's
scheduler. Instead, you point a periodic trigger your OS provides (cron, a
systemd timer, launchd, Windows Task Scheduler) at one command:

```sh
rest-o-matic tick
```

Each `tick` checks every job's schedule against its last recorded run, runs
whichever are due, and exits. Run it every one to five minutes.

## Schedules

A job's schedule comes from its policy and is one of three values. There is
no time-of-day setting.

| `schedule` | A job becomes due |
|---|---|
| `hourly` | At the top of each hour |
| `daily` | At 00:00 UTC each day |
| `weekly` | At 00:00 UTC each Sunday |

- A due job starts at the first `tick` after its boundary, so with a
  five-minute trigger a daily job starts between 00:00 and 00:05 UTC.
- Boundaries are in UTC, not your local time zone. `status` shows the next
  due time in local time.
- A job that missed its window (the machine was asleep, say) catches up
  once on the next tick. It doesn't run once per missed period.
- A run counts for its period whatever its outcome. A job that fails is not
  tried again until its next period; use `rest-o-matic run <job>` to retry
  sooner.

## Running a job now

To run a specific job right now, whether or not it's due:

```sh
rest-o-matic run documents
```

## Triggering tick with cron

```cron
PATH=/home/me/.local/bin:/usr/local/bin:/usr/bin:/bin
*/5 * * * * cd /home/me/backups && rest-o-matic --state-dir /home/me/backups/.rest-o-matic tick
```

- Put the entry in the crontab of the user that should run the backups:
  root for rootful Docker data, the container user for rootless Podman.
- cron runs with a minimal `PATH` that usually leaves out `~/.local/bin`,
  where the install script puts rest-o-matic, and sometimes
  `/usr/local/bin`. Set it as above so that both `rest-o-matic` and `restic`
  are found, or use full paths.
- `tick` reads `rest-o-matic.yaml` from the working directory, hence the
  `cd`. Give `--state-dir` as an absolute path (see
  [One user per state directory](concurrency.md#one-user-per-state-directory)).
- Overlapping ticks are safe. If a backup is still running when the next
  tick fires, that job is skipped and any others that are due still start
  (see [Concurrency](concurrency.md)).

### Keeping the output

cron emails a job's output to the local user, but only if the machine has a
mail server, and most don't. Without one the output is thrown away, so a
failure leaves no trace. Either append it to a file:

```cron
*/5 * * * * cd /home/me/backups && rest-o-matic tick >> /home/me/backups/tick.log 2>&1
```

or, on a system with systemd, send it to the journal, which rotates itself:

```cron
*/5 * * * * cd /home/me/backups && rest-o-matic tick 2>&1 | systemd-cat -t rest-o-matic
```

and read it with `journalctl -t rest-o-matic`.

Whichever you choose, [`status`](status.md) shows the result of every job
without reading any log, and [notifications](notifications.md) can tell you
when one fails.

## Triggering tick with a systemd timer

A timer keeps the output in the journal with no extra setup. For a user's
own backups, create two files in `~/.config/systemd/user/`:

```ini
# rest-o-matic.service
[Unit]
Description=rest-o-matic tick

[Service]
Type=oneshot
WorkingDirectory=%h/backups
ExecStart=%h/.local/bin/rest-o-matic --state-dir %h/backups/.rest-o-matic tick
TimeoutStopSec=120
```

```ini
# rest-o-matic.timer
[Unit]
Description=Run rest-o-matic tick every five minutes

[Timer]
OnCalendar=*:0/5
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now rest-o-matic.timer
loginctl enable-linger "$USER"        # keep it running when you're logged out
journalctl --user -u rest-o-matic     # the output
```

Two things differ from cron:

- systemd does not start a tick while the previous one is still running.
  During a long backup, other jobs that become due wait until it finishes,
  where cron would start them from an overlapping tick. If you have a
  backup that runs for hours alongside jobs that must run hourly, use cron.
- `TimeoutStopSec=120` gives an interrupted job time to run its cleanup
  hooks (which may take up to 60 seconds) before systemd kills it.

rest-o-matic doesn't create these files for you.

## Other schedulers

launchd on macOS and Task Scheduler on Windows work the same way: run
`rest-o-matic tick` every few minutes, from the directory holding the
config, as the user that owns the data.

## Where state is kept

`tick` knows what is due from a state file, which holds each job's recent
runs and the time of the last tick. It lives in `.rest-o-matic/state.json`
by default; `--state-dir` moves it. The file holds no backup data. See
[Concurrency and the state directory](concurrency.md).
