# rest-o-matic

[![CI](https://github.com/drewlsvern/rest-o-matic/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/drewlsvern/rest-o-matic/actions/workflows/ci.yml?query=branch%3Amain)

A small wrapper around [restic](https://restic.net/) for backing up a server
on a schedule. You describe what to back up, where to, and how often in one
YAML file; rest-o-matic runs restic for you, applies retention, and tells
you whether it worked.

- **One config file**, built from four parts: what to back up (a source),
  where to (a repository), how often and how long to keep it (a policy),
  and what to run around it (hooks).
- **No daemon.** Your own scheduler runs one command every few minutes, and
  rest-o-matic works out which jobs are due.
- **One command to see if it's working**, with each job's last result, its
  error, when it runs next and the snapshots it has.
- **Alerts** when a job starts failing and when it recovers, without one per
  run.
- **Passwords can be stored encrypted**, so the config is safe to keep in
  git.
- **Container data**, including files only rootless Podman can read.

## Install

You need [restic](https://restic.net/) on your `PATH`; rest-o-matic runs it
rather than reimplementing it.

On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.ps1 | iex
```

Both download the newest release, verify its checksum, and install it for
your user. For a system-wide install, a specific version, manual downloads
or building from source, see [Installation](docs/installation.md).

## Quick start

**1. Write a config.** Save this as `rest-o-matic.yaml`, changing the paths:

```yaml
policies:
  daily:
    schedule: daily
    retention: {daily: 7, weekly: 4}

repositories:
  nas:
    backend: local
    url: /mnt/nas/restic-repo
    password: correct-horse-battery-staple

backups:
  documents:
    source:
      paths: ["/home/me/documents"]
    policy: daily
    repositories: [nas]
```

A leading `~` in a source path means your home directory. Nothing else is
expanded, so write the rest of each path in full.

**2. Check it.**

```sh
rest-o-matic validate
```

**3. Create the repository.** This is needed once per repository.
rest-o-matic never creates one for you.

```sh
rest-o-matic exec nas -- init
```

**4. Take the first backup.**

```sh
rest-o-matic run documents
```

```
job documents: OK
  repository nas: ok
```

**5. See how it went.**

```sh
rest-o-matic status
```

```
last tick: never

JOB        SCHEDULE  LAST RUN  OUTCOME  TOOK  NEXT DUE
documents  daily     just now  ok       1s    in 7 hours
```

**6. Schedule it.** Have cron run `tick` every few minutes, from the
directory holding the config:

```cron
PATH=/home/me/.local/bin:/usr/local/bin:/usr/bin:/bin
*/5 * * * * cd /home/me/backups && rest-o-matic --state-dir /home/me/backups/.rest-o-matic tick
```

`tick` runs whichever jobs are due and exits. See
[Scheduling](docs/scheduling.md) for what `daily` means exactly, how to keep
the output, and a systemd timer.

**7. Restore something,** to be sure you can.

```sh
rest-o-matic exec nas -- snapshots
rest-o-matic exec nas -- restore <snapshot-id> --target /tmp/restore
```

From here, the two things most worth adding are
[notifications](docs/notifications.md), so that a failed backup tells you,
and [locked secrets](docs/secrets.md), so that the password isn't in the
config in plain text.

## Commands

| Command | What it does |
|---|---|
| `validate` | Checks the config without running anything |
| `run <job>` | Runs one job now |
| `tick` | Runs every job that is due; this is what your scheduler calls |
| `status [job]` | Shows each job's last run, outcome and next due time |
| `exec <repository> -- ...` | Runs any restic command against a repository |
| `secret ...` | Creates the host key, and locks and reveals config values |

The full list of flags and exit codes is in the
[command reference](docs/cli.md).

## Documentation

| Page | What's in it |
|---|---|
| [Installation](docs/installation.md) | Install scripts, manual downloads, building from source |
| [Configuration](docs/configuration.md) | Sources, repositories, policies, retention overrides, hooks |
| [Scheduling](docs/scheduling.md) | Schedules, cron and systemd timer setups, keeping the output |
| [Status](docs/status.md) | Reading `status`, its JSON output |
| [Notifications](docs/notifications.md) | Being told when a job fails and when it recovers |
| [Secrets](docs/secrets.md) | Storing passwords and credentials encrypted |
| [Backing up container data](docs/containers.md) | Rootless Podman, rootful Docker, restoring |
| [Concurrency and the state directory](docs/concurrency.md) | What runs at the same time, waiting, one user per state directory |
| [Running restic commands](docs/restic-commands.md) | `exec`, initialising, restoring, its safeguards and exit codes |
| [Command reference](docs/cli.md) | Every command, global flags, exit status, colours |
| [Development](docs/development.md) | Building, tests, versioning, releasing |

[`rest-o-matic.example.yaml`](rest-o-matic.example.yaml) lists every config
option, commented.

## What's not here yet

- Container, Quadlet and command source types. Only `paths` sources are
  supported; container data is backed up through its bind mounts (see
  [Backing up container data](docs/containers.md)).
- Built-in stopping and starting of containers (use hooks), named volumes,
  and rootless Docker.
- Setting up your scheduler for you. rest-o-matic never writes a crontab
  entry or a systemd unit; the docs show working examples.

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
