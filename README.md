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
go install github.com/drewlsvern/rest-o-matic/cmd/rest-o-matic@latest
```

Or build from source:

```sh
go build -o rest-o-matic ./cmd/rest-o-matic
```

You'll also need the [`restic`](https://restic.net/) binary on your `PATH` —
rest-o-matic shells out to it rather than reimplementing any backend logic.

## Version

Running `rest-o-matic` with no arguments shows a short version line above the
usual command listing; `rest-o-matic --version` (or `-v`) shows a fuller
build-info block — commit, commit time, whether the working tree was dirty at
build time, and the Go version used to build it.

The version is derived entirely from Go's own automatic build-info stamping
(no hand-maintained version number, no build script required) — `go build`
alone is enough for it to work correctly, both for an untagged dev build and,
later, for a build made at a tagged release commit. The one thing it depends
on is building from an actual `.git` checkout — building from a source
archive with no `.git` directory (e.g. GitHub's "Download ZIP") won't have
commit/time/dirty detail available.

## Config

By default rest-o-matic reads `rest-o-matic.yaml` in the current directory
(override with `--config`). See [`rest-o-matic.example.yaml`](rest-o-matic.example.yaml)
for every option this version understands, commented out and annotated -
copy what you need from it. Here's the shape, covering both a plain-file
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

## Running raw restic commands

For anything `backup`/`forget` don't cover — `snapshots`, `check`, `restore`,
`mount`, `diff`, `dump`, and so on — `exec` injects a repository's connection
info and passes the rest straight to the real `restic` binary:

```sh
rest-o-matic exec nas -- snapshots
rest-o-matic exec nas -- restore a1b2c3d4 --target /tmp/restore
```

Output and the exit code are restic's own, completely unmodified — `exec` is
a transparent passthrough, not a parsed/reinterpreted wrapper like
`backup`/`forget`.

Two safeguards apply, and they're independent of each other:

- **Locking** mirrors restic's own shared/exclusive lock model rather than
  an invented "dangerous command" list: read-oriented subcommands
  (`snapshots`, `ls`, `find`, `diff`, `stats`, `cat`, `check`, `mount`,
  `restore`) run immediately; everything else — including any subcommand
  rest-o-matic doesn't recognize — waits for the same per-repository lock
  `backup`/`forget` use, and is skipped (not run) if another execution
  currently holds it. `--force` skips this wait only; restic's own internal
  locking is still the real backstop either way.
- **A tag-safety gate**, with no override flag at all: `forget` and `tag`
  are refused outright — and `restore latest` refused when no explicit
  snapshot ID is given — against a repository more than one job references,
  unless your own command already includes `--tag`. This is what stops an
  unscoped `forget --prune` from quietly pruning a different job's snapshots
  out of a repository they share. `--force` does **not** bypass this — the
  only ways around it are adding `--tag <job-name>` yourself, or running
  `restic` directly outside of `exec`.

When `exec` refuses to invoke restic at all, it uses one of two reserved
exit codes instead of restic's own: `20` means it was blocked by the lock
(retry later, or use `--force`), `21` means it was blocked by the
tag-safety gate (the command itself needs `--tag`, not a retry). Whenever
restic is actually invoked, its own exit code is returned unchanged instead.

Currently, a lock-blocked message just says the repository is "in use by
another execution" — it doesn't yet say which job or process holds it.
Surfacing that is a planned future enhancement, not implemented yet.

## Releasing

Every pull request into `main` runs a build/test check automatically
(`.github/workflows/ci.yml`); merging is blocked until it passes.

Cutting a release is a manual, deliberate action — nothing tags or publishes
a release automatically just because something merged. To release:

```sh
git tag v1.2.3
git push origin v1.2.3
```

Pushing a tag matching `vMAJOR.MINOR.PATCH` (optionally with a `-prerelease`
suffix, e.g. `v1.2.3-rc.1`) triggers `.github/workflows/release.yml`, which
validates the tag format, verifies the tagged commit is actually part of
`main`'s history, then uses [GoReleaser](https://goreleaser.com/) to
cross-compile binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64`, and `windows/amd64`, publish them with checksums and a
changelog (grouped by conventional-commit type) to a GitHub Release, and
mark a hyphenated pre-release tag as a pre-release automatically.

## What's not here yet

- Container/Podman/Docker source types (`container`, `quadlet`, `command`
  sources) — v1 only supports `paths` sources.
- Any systemd unit generation — that's reserved for the future Podman
  Quadlet integration specifically.
- rest-o-matic managing your crontab for you — you own that entry.

## License

Copyright (C) 2026 Joel Drewlo

rest-o-matic is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License as published by the Free
Software Foundation, either version 3 of the License, or (at your option)
any later version.

rest-o-matic is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the [GNU General Public License](LICENSE)
for more details.
