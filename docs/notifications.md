# Notifications

A job's hooks belong to that job. To be told about **any** job, without
adding a hook to each one, put commands in a top-level `notify` block:

```yaml
notify:
  failure:  ["curl -fsS -d \"$RESTOMATIC_JOB failed: $RESTOMATIC_ERROR\" https://ntfy.example.com/backups"]
  recovery: ["curl -fsS -d \"$RESTOMATIC_JOB is working again\" https://ntfy.example.com/backups"]
  success:  ["curl -fsS https://hc-ping.com/<ping-key>/$RESTOMATIC_JOB"]
```

They apply to every job, whether `tick` or `run` started it:

- `failure` runs when a job **starts** failing, and then at most once every
  24 hours for as long as it keeps failing. An hourly job that fails all day
  sends one alert, not 24. The limit is kept per job.
- `recovery` runs once, on the first successful run after a failure.
- `success` runs after every successful run. Use it for healthcheck pings,
  which have to arrive every time.

They run after the job's own hooks and never change the job's outcome; a
notification command that fails is reported as a warning. If a `failure`
command itself fails (the notification service is down, say), it is tried
again on the job's next failed run instead of waiting out the day.

Notification commands get the same variables as a job's `success` and
`failure` hooks, plus `RESTOMATIC_FAILING_SINCE`: when the first run of the
current run of failures started (UTC, e.g. `2026-01-05T02:00:00Z`). It is
empty for `success`.

A job's own `hooks` are unchanged and still run on every run. Use those for
things the backup needs (stopping a container, dumping a database), and
`notify` for telling someone.

What notifications can't tell you: they only run when rest-o-matic runs and
reaches a job. If the scheduler stops firing, the host is down, or the
config no longer validates, nothing runs and nothing is sent. Check the
`last tick` line of [`status`](status.md) for the first of those.
