# rest-o-matic

A simple [restic](https://restic.net/) wrapper. rest-o-matic separates four
concepts that most restic wrappers blur together:

- **Source** — what to back up.
- **Repository** — where a restic repository lives.
- **Policy** — how and when a job runs (schedule + retention), named and
  reusable across jobs.
- **Hooks** — commands to run before/after a job.

This is the initial release: the core wrapper only. Container integrations
(Podman Quadlets, Docker Compose) and any systemd-based tooling are planned
for later and are **not** part of this release — see "What's not here yet"
below.

## Installation

```sh
go build -o rest-o-matic ./cmd/rest-o-matic
```

You'll also need the [`restic`](https://restic.net/) binary on your `PATH` —
rest-o-matic shells out to it rather than reimplementing any backend logic.

## Config

By default rest-o-matic reads `rest-o-matic.yaml` in the current directory
(override with `--config`). Here's the shape, covering both a plain-file
backup and an application-aware one with hooks and per-repository retention:

```yaml
max_concurrent: 2   # how many jobs may run at once across the whole host

policies:
  hot:
    schedule: hourly
    retention: {hourly: 24, daily: 30, weekly: 12}

repositories:
  nas:
    backend: local
    url: /mnt/nas/restic-repo
    password: correct-horse-battery-staple
  offsite:
    backend: s3
    url: "s3:https://s3.example.com/my-bucket/restic-repo"
    password_command: "pass show restic/offsite"
    env:
      AWS_ACCESS_KEY_ID: AKIA...
      AWS_SECRET_ACCESS_KEY: ...

backups:
  documents:
    source:
      paths: ["~/documents"]
    policy: hot
    repositories: [nas, offsite]

  postgres:
    source:
      paths: ["/var/backups/postgres"]
    hooks:
      before: ["pg_dump mydb > /var/backups/postgres/mydb.sql"]
      after: ["rm -f /var/backups/postgres/mydb.sql"]
    policy: hot
    retention: {weekly: 8}          # overrides `hot`'s weekly count for this job
    repositories:
      - nas
      - repo: offsite
        retention: {daily: 10}      # overrides again, just for this repository
    tags: [prod]                    # added alongside the automatic job-name tag
```

Every snapshot is automatically tagged with its job's name (so `nas` can
safely hold snapshots from both `documents` and `postgres` — `forget` is
always scoped to a job's own tag). Retention resolves through up to three
levels, each overriding only the keys it mentions: the named policy, then an
optional job-level override, then an optional per-repository override.

Validate a config without running anything:

```sh
rest-o-matic validate
```

## Scheduling

rest-o-matic never runs as a daemon and never touches your system's
scheduler. Instead, point **any** periodic trigger your OS provides — cron,
launchd, Windows Task Scheduler, whatever — at one command:

```cron
*/5 * * * * cd /path/to/config && rest-o-matic tick
```

Each `tick` invocation checks every job's schedule against its last
recorded run, runs whichever are due, and exits. A job that missed its
window (e.g. the machine was asleep) catches up once on the next tick — it
doesn't fire once per missed occurrence. State (last run time and outcome
per job) is kept in `.rest-o-matic/state.json` by default (override with
`--state-dir`).

To run a specific job right now, regardless of whether it's due:

```sh
rest-o-matic run documents
```

## Concurrency

Two rules govern which jobs run at the same time, both enforced with
ordinary file locks so they hold even across independently invoked
processes (two overlapping ticks, or a tick and a manual `run`):

- Two jobs never back up to the **same repository** at the same time —
  restic's own repository locking makes this a correctness requirement, not
  just a performance one.
- No more than `max_concurrent` jobs run at once, period.

## What's not here yet

- Container/Podman/Docker source types (`container`, `quadlet`, `command`
  sources) — v1 only supports `paths` sources.
- Any systemd unit generation — that's reserved for the future Podman
  Quadlet integration specifically.
- rest-o-matic managing your crontab for you — you own that entry.
