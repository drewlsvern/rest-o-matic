# Configuration

By default rest-o-matic reads `rest-o-matic.yaml` in the current directory
(override with `--config`). See [`rest-o-matic.example.yaml`](../rest-o-matic.example.yaml)
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
      paths: ["/home/me/documents"]
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

Source paths are passed to restic with no shell involved. A leading `~`
(alone, or as `~/...`) is replaced with the home directory of the user
running rest-o-matic; nothing else is expanded, so `~alice/...` and
environment variables such as `$HOME` are taken literally. Be aware that
under `sudo`, `~` follows whatever `HOME` sudo leaves set, which may be
root's. For anything scheduled, absolute paths leave no room for doubt.

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

A repository has to be initialised once before its first backup; see
[Running restic commands](restic-commands.md#initialising-a-repository).

A policy's `schedule` is `hourly`, `daily` or `weekly`; see
[Scheduling](scheduling.md#schedules). Passwords and other credentials can
be stored encrypted; see [Secrets](secrets.md).

## Hooks

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

## Checking a config

Validate a config without running anything:

```sh
rest-o-matic validate
```

It reports every problem with its line in the file. Errors stop the config
from being used; warnings point at something that is probably wrong. `tick`,
`run` and `status` run the same checks and print the same problems.

`validate` needs nothing from the machine it runs on (no host key, state
directory or source paths), so a config can be checked anywhere before it is
copied to its host. The one exception is `read_as: podman-unshare`, which is
checked against the operating system and whether podman is installed.
`validate --json` prints the result as one JSON document, for programs; its
format is described in
[`contract/validate/v1`](../contract/validate/v1/README.md).

### Misspelt and unknown keys

A key rest-o-matic doesn't read is reported as a warning, with its line and
the key you probably meant:

```
config warning: line 9: job "docs": "hook" is not a key rest-o-matic reads, and is ignored; did you mean "hooks"?
```

The key is ignored, not corrected, so in that example the job runs without
its hooks until the key is fixed. Take these warnings seriously: a misspelt
`hooks`, `retention` or `password_file` changes what a job does.

Top-level keys starting with `x-` are not read and are never warned about.
They are the place to define YAML anchors that the rest of the config
reuses:

```yaml
x-shared:
  s3: &s3
    backend: s3
    env: {AWS_DEFAULT_REGION: eu-west-1}

repositories:
  offsite:
    <<: *s3
    url: "s3:https://s3.example.com/bucket"
```

Names under a repository's `env` are environment variables of your choosing
and are never checked.

### Editor support

`rest-o-matic schema` prints a JSON Schema of the config, which editors with
YAML support use for completion, descriptions on hover, and marking mistakes
as you type. Each release prints the schema for its own config. In VS Code,
with the YAML extension:

```sh
rest-o-matic schema > ~/.config/rest-o-matic/config.schema.json
```

```json
// settings.json
"yaml.schemas": {"/home/me/.config/rest-o-matic/config.schema.json": "rest-o-matic*.yaml"},
"yaml.customTags": ["!locked scalar", "!plain scalar"]
```

The custom tags tell the editor that `!locked "..."` and `!plain "..."` are
ordinary values. Editors built on the same language server, such as Monaco
with monaco-yaml, take the same two settings.
