# rest-o-matic

A simple [restic](https://restic.net/) wrapper. rest-o-matic separates four
concepts that most restic wrappers blur together:

- **Source** — what to back up.
- **Repository** — where a restic repository lives.
- **Policy** — how and when a job runs (schedule + retention), named and
  reusable across jobs.
- **Hooks** — commands to run before/after a job.

Container data can be backed up through its bind mounts, including files
owned by rootless Podman's subordinate uids (see "Container volumes").
Deeper container integrations (Podman Quadlets, Docker Compose) and any
systemd-based tooling are planned for later — see "What's not here yet"
below.

## Installation

**Recommended for actually running backups** (a server, NAS, anything that
isn't just your own dev machine): download the pre-built binary from the
[Releases page](https://github.com/drewlsvern/rest-o-matic/releases). No Go
toolchain needed on that machine, it's the exact artifact that was built and
tested, checksums are provided, and `--version` on it reports full build
provenance (real commit hash and build time — see "Version" below).

On Linux or macOS, [`install.sh`](install.sh) does this for you. It
downloads the newest release for your OS and CPU, verifies its checksum,
and installs it into `~/.local/bin`, or into `/usr/local/bin` when run as
root:

```sh
curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sh        # just you
curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sudo sh   # system-wide
# a specific release, or a different directory:
curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sh -s -- --version v0.0.1-rc.3 --dir ~/bin
```

`--list` shows the available releases. It's plain POSIX `sh` and needs only
`curl` or `wget`, `tar`, and `sha256sum` or `shasum`. Or do it by hand:

```sh
curl -LO https://github.com/drewlsvern/rest-o-matic/releases/download/<version>/rest-o-matic_<version>_<os>_<arch>.tar.gz
curl -LO https://github.com/drewlsvern/rest-o-matic/releases/download/<version>/checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar -xzf rest-o-matic_<version>_<os>_<arch>.tar.gz
```

(`<os>_<arch>` is one of `linux_amd64`, `linux_arm64`, `darwin_amd64`,
`darwin_arm64`, or `windows_amd64` — see the Releases page for the current
`<version>` and exact filenames; Windows archives are `.zip` instead.)

On Windows, [`install.ps1`](install.ps1) does the same from PowerShell
(Windows PowerShell 5.1 or PowerShell 7):

```powershell
irm https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.ps1 | iex
# a specific release, or a different folder:
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.ps1))) -Version v0.0.1-rc.3 -InstallDir C:\Tools\rest-o-matic
```

It installs just for you into `%LOCALAPPDATA%\Programs\rest-o-matic`, or
for everyone into `Program Files\rest-o-matic` when run from an elevated
(administrator) PowerShell, and adds that folder to your user or the machine
`PATH` (`-NoPath` skips this). `-List` shows the available releases.

**For quickly trying a version on a machine that already has Go installed**:

```sh
go install github.com/drewlsvern/rest-o-matic/cmd/rest-o-matic@latest
```

This compiles from source fetched through Go's module system rather than
using the pre-built binary, so the only trade-off is that `--version`'s
commit/build-time fields show as "unknown" (see "Version" below) — the
version number itself is still always correct.

Or build from source yourself:

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

Each repository's `backend` is required and must match its `url`'s scheme
prefix (`backend: s3` needs a url starting with `s3:`, `sftp` needs `sftp:`,
and so on; `local` needs a plain path). A mismatch is a config error, because
restic would otherwise quietly treat the url as a local directory. `url` itself
is always passed to restic unchanged. An unrecognised backend or a relative
local path produces a warning rather than an error.

### Hooks

`before` commands run first; if one fails, the rest are skipped and no
backup runs. `after` runs once every repository has been attempted, and can
be a plain list (run every time) or a map that splits by outcome:

```yaml
hooks:
  before: ["update-container.sh stop app"]
  after:
    always:  ["update-container.sh start app"]         # every time, first
    success: ["curl -fsS https://hc-ping.com/<uuid>"]   # then, if all ok
    failure: ["notify.sh \"$RESTOMATIC_JOB: $RESTOMATIC_ERROR\""]  # or, if not
```

- The order is always `before` → backups → `always` → `success` *or*
  `failure`, whatever order you write the keys in. `after: [...]` as a plain
  list means `always`.
- A failing `always` command fails the job, so a container that didn't come
  back up triggers `failure`. A failing `success`/`failure` command is
  reported but doesn't change the outcome.
- Every command in `always`, `success` and `failure` runs even if an earlier
  one fails.
- Hooks get `RESTOMATIC_JOB`; `success`/`failure` also get
  `RESTOMATIC_OUTCOME`, `RESTOMATIC_FAILED_REPOS` and `RESTOMATIC_ERROR`.
- If rest-o-matic is stopped (SIGTERM or Ctrl-C), it stops the running backup,
  starts no further jobs, then runs `always` and `failure` for up to 60
  seconds before exiting; the job is recorded as failed. A second signal skips
  that cleanup. Under systemd, set `TimeoutStopSec=` above 60 (e.g. `120`) so
  the cleanup isn't cut short by SIGKILL.

Each hook is one command line run by the platform's shell: `sh -c` on Linux
and macOS, `cmd.exe` on Windows. Write hooks in that shell's syntax, e.g.
environment variables are `$RESTOMATIC_JOB` on Linux/macOS but
`%RESTOMATIC_JOB%` on Windows. Simple commands work the same on both:

```yaml
# Windows
after:
  success: ["curl -fsS --retry 5 -m 10 https://hc-ping.com/<uuid>"]
  failure: ['curl -fsS -m 10 -d "%RESTOMATIC_JOB% failed" https://hc-ping.com/<uuid>/fail']
```

For anything more involved on Windows, call a script from the hook
(`powershell -NoProfile -File C:\scripts\notify.ps1`). When a Windows hook
has to be stopped (an interrupt, or the cleanup time limit), it and everything
it started are killed immediately, rather than asked to exit first as on
Linux/macOS.

Validate a config without running anything:

```sh
rest-o-matic validate
```

## Container volumes

rest-o-matic has no container-specific source type yet. To back up the data
of a container, run rest-o-matic **on the container host** and list the
host side of its bind mounts under `paths:`, with absolute paths. For
example, a container started with
`-v /home/me/containers/gitea/data:/data` is backed up with
`paths: [/home/me/containers/gitea]`. Stop and start containers with hooks
(see [Hooks](#hooks)) if their data must not change during the backup.
Named volumes aren't supported yet.

What decides whether this works is **who owns the files** the containers
write:

| Runtime | Run rest-o-matic as | `read_as` |
|---|---|---|
| Rootless Podman | the user that runs the containers | `podman-unshare` (see below) |
| Rootful Docker or rootful Podman | root | leave unset |
| Rootless Docker | — | not supported yet |

### Rootless Podman: `read_as: podman-unshare`

Rootless Podman maps users inside a container to your subordinate uids
(`/etc/subuid`), so a container's `postgres` user (uid 999) writes files
owned by something like uid 100998 on the host. When those files are
private (`0600`, or a `0700` directory, as Postgres uses), you can't read
them as yourself, and the backup fails with restic's exit code 3 and
`permission denied`. rest-o-matic adds a hint pointing here when that
happens.

`--userns=keep-id` maps the container's user to *you*, so a container
running as one user under keep-id writes files you can read. But a
container whose entrypoint runs as root before switching users, an image
running as a different uid, or a pod whose containers run as different
users still produces files you can't read. Rather than working out which
case applies, set:

```yaml
backups:
  gitea:
    source:
      paths: [/home/me/containers/gitea]
    read_as: podman-unshare
    policy: daily
    repositories: [nas]
```

restic then runs as `podman unshare restic backup …`, inside your rootless
user namespace, where every file your containers wrote is readable. This
needs no extra privilege, and works the same with or without keep-id. Only
the backup runs this way; hooks and retention don't. Snapshots taken this
way are tagged `restomatic-read=podman-unshare`. `read_as` is Linux-only:
on macOS and Windows, Podman runs containers in a VM and bind-mounted files
are already readable.

**Restore through the job**, so files come back with the owners they had.
Restore into an empty scratch directory first, then move what you need into
place:

```sh
# 1. restore into a scratch directory
rest-o-matic exec nas --job gitea -- restore <snapshot-id> \
  --target /home/me/restore --include /home/me/containers/gitea/data/app.ini

# 2. move it into place (inside the namespace, where the files are yours)
podman unshare mv /home/me/restore/home/me/containers/gitea/data/app.ini \
                  /home/me/containers/gitea/data/app.ini
podman unshare rm -rf /home/me/restore
```

Don't restore straight to `--target /`. restic also restores the owner and
permissions of every parent directory on the path (`/home`, `/home/me`,
…), which under `podman unshare` can't be set for `/` and `/home` (so the
restore reports errors), and goes wrong for your own directories if the
snapshot was read differently.

`--job` runs restic the way that job reads its files. A snapshot records
ownership as it was seen at backup time, so it must be restored the same
way. For a `podman-unshare` job, `exec --job` refuses (exit code 23) to
restore a snapshot that lacks the `restomatic-read=podman-unshare` tag,
and for `latest` it requires that tag in every `--tag` filter
(`--tag gitea,restomatic-read=podman-unshare`). Restore snapshots taken
before the job switched to `podman-unshare` with plain `exec`, without
`--job`. `restic mount` may not work under `--job`; use `restore`, `dump`
or `ls` instead.

### Rootful Docker or Podman: run as root

The containers already run as root, so run rest-o-matic as root too (from
root's crontab or a system timer). Root can read every file, and restores
bring back the original owners. Keep one user per state directory (see
[One user per state directory](#one-user-per-state-directory)).

If you'd rather not run the whole thing as root, you can let a normal user's
restic read every file instead. This is an advanced option:

```sh
sudo groupadd restic && sudo usermod -aG restic me
sudo chown root:restic /usr/local/bin/restic && sudo chmod 750 /usr/local/bin/restic
sudo setcap cap_dac_read_search+ep /usr/local/bin/restic
```

This lets members of the `restic` group read any file on the host through
restic, and nothing more. The capability is lost whenever the restic binary
is replaced, so reapply it after upgrades. Restoring files with their
original owners still needs root (`sudo rest-o-matic exec …`).

## Notifications

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
`last tick` line of [`status`](#status) for the first of those.

## Secrets

A repository's `password`, and any value under its `env`, can be written in
a **locked** form instead of plain text:

```yaml
repositories:
  offsite:
    backend: s3
    url: "s3:https://s3.example.com/my-bucket/restic-repo"
    password: !locked "YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+IFgyNTUxOSA..."
    env:
      AWS_ACCESS_KEY_ID: AKIA...                    # plain, your choice per value
      AWS_SECRET_ACCESS_KEY: !locked "YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+..."
```

A locked value is encrypted for this host's key. rest-o-matic unlocks it in
memory when it starts restic and never writes the plain text to disk, so the
config file can be copied, kept in git or stored elsewhere without exposing
the credentials in it. Plain-text values keep working, and the two can be
mixed. `password_file` and `password_command` are unchanged.

**1. Make a recovery key first.** A value locked only for a host is lost if
that host's key is. Create a second key with [age](https://age-encryption.org),
keep its private half somewhere safe that isn't this host (a password
manager), and list its public half beside the host key:

```sh
age-keygen                       # prints a private key and its "public key: age1..."
mkdir -p ~/.config/rest-o-matic
echo "age1...the public key..." > ~/.config/rest-o-matic/recovery-recipients
```

Every value locked on this host from then on can also be opened with the
recovery key. The file takes one public key per line.

**2. Create the host's key.**

```sh
rest-o-matic secret keygen
```

It is written to `host.key` in your user config directory
(`~/.config/rest-o-matic/` on Linux), readable only by you, and is never
overwritten. `--key-file` uses a different path, with `recovery-recipients`
looked for in the same directory. The key is per user, so run this as the
user that runs your backups. A key file that other users can read is
refused.

**3. Lock a value and paste it into the config.**

```sh
rest-o-matic secret lock         # prompts for the value without showing it
pass show restic/offsite | rest-o-matic secret lock
```

It prints `!locked "..."`, which goes after `password:` or an `env` name.
To lock a value for a different host, add `--recipient <that host's public
key>` (from `rest-o-matic secret public-key` on that host).

Other commands:

- `rest-o-matic secret check` tries to unlock every locked value in the
  config and lists any this host can't open. Run it after copying a config
  to a host. (`validate` only checks that locked values are well-formed, and
  needs no key, so it gives the same result on any machine.)
- `rest-o-matic secret reveal <repository>` prints a locked password, and
  `secret reveal <repository> <ENV_NAME>` an env value, for running restic
  by hand.

A repository whose locked value can't be unlocked fails without restic being
started, and says whether the host has no key or the value was locked for a
different one.

Locked values are ordinary age files, base64-encoded, so they can be opened
without rest-o-matic:

```sh
echo '<the text between the quotes>' | base64 -d | age --decrypt -i <key file>
```

What locking does and doesn't protect: it protects the config once it leaves
the host. It does not protect against someone who can already read files as
the backup user there, since they can read the host key too. Only `password`
and `env` values can be locked; if a repository `url` contains credentials,
move them into `env`.

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
doesn't fire once per missed occurrence. State (each job's recent
runs, and the time of the last tick) is kept in `.rest-o-matic/state.json` by default (override with
`--state-dir`).

To run a specific job right now, regardless of whether it's due:

```sh
rest-o-matic run documents
```

## Status

`rest-o-matic status` shows what has been recorded about every job, without
running anything or touching a repository:

```
last tick: 2 minutes ago

JOB        SCHEDULE  LAST RUN        OUTCOME  TOOK   NEXT DUE
documents  daily     6 hours ago     ok       1m12s  in 18 hours
gitea      daily     2 hours ago     FAILED   48s    in 22 hours
  nas: ok (snapshot a1b2c3d4)
  offsite: backup failed: restic backup failed: exit status 3: ...
media      weekly    never           -        -      due now
postgres   hourly    1 hour ago      ok       8s     due since 19:00
  running since 3 minutes ago (started by tick)
```

- **last tick** is when `tick` last ran. If it's older than your cron
  interval, the scheduler has stopped firing.
- **NEXT DUE** is when `tick` will next start the job. Schedule boundaries
  are in UTC (a daily job is due from 00:00 UTC); they are shown here in
  your local time.
- A failed job lists the result for each repository it tried, or the error
  of whatever else failed (a hook, an interruption).
- A job that has started is shown as `running`; one queued behind a busy
  repository or slot is shown as `waiting` (see [Concurrency](#concurrency)).

`rest-o-matic status <job>` lists that job's recent runs, newest first. The
last 20 are kept.

`rest-o-matic status --json` prints the same information as a single JSON
document, with full snapshot IDs and untruncated errors, for scripts and
monitoring. Times are UTC, and values that don't apply are `null`.

`status` exits 0 whenever it could report, whatever the health of the jobs,
and only reads: it is safe to run as any user that can read the state
directory.

## Concurrency

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
below).

### One user per state directory

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
  last-run times and locks.

`exec` subcommands that don't take the lock (`snapshots`, `restore` and the
other read-oriented ones, or anything with `--force`) create no files there,
so they aren't checked. `validate` isn't checked either. The check doesn't
apply on Windows.

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

When `exec` refuses to invoke restic at all, it uses one of four reserved
exit codes instead of restic's own: `20` means it was blocked by the lock
(retry later, or use `--force`), `21` means it was blocked by the
tag-safety gate (the command itself needs `--tag`, not a retry), and `22`
means the state directory belongs to another user (see
[One user per state directory](#one-user-per-state-directory)), and `23`
means a restore through `--job` would use a snapshot taken in a different
read mode (see [Container volumes](#container-volumes)). Whenever
restic is actually invoked, its own exit code is returned unchanged instead.

Currently, a lock-blocked message just says the repository is "in use by
another execution" — it doesn't yet say which job or process holds it.
Surfacing that is a planned future enhancement, not implemented yet.

## Output colours

When run in a terminal, rest-o-matic colours its own status labels: errors
red, warnings orange, successes green. Only the label is coloured, and restic's
own output is never touched. Colour is decided separately for stdout and
stderr, so it's on only for a stream that's a terminal. Output going to cron
mail, the systemd journal, a pipe or a file stays exactly as plain as before.

- `--color=auto` (default): colour only on a terminal
- `--color=always` / `--color=never`: force it on or off
- `NO_COLOR=1`: turn it off, unless `--color=always` is given

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
  sources) — v1 only supports `paths` sources. Container data is backed up
  through its bind mounts; see [Container volumes](#container-volumes).
- Built-in stopping and starting of containers (use hooks), named volumes,
  and rootless Docker.
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
