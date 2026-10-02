# Central management: architecture design

Status: draft, 2026-10-01. Nothing here is implemented. This document records
the architecture agreed in discussion so that the individual changes built
from it stay consistent. The [decision log](#decision-log) lists what was
decided and what is still open.

The work is delivered as a series of separate changes; see
[Delivery sequence](#delivery-sequence). The central app will live in its own
repository, and its own proposal will be written there. This document defines
what the host side does and the contract between the two.

## Why

rest-o-matic runs on each server and pushes to that server's own
repositories. With up to ten or more hosts, three things are painful today:

1. **Checking that backups work** means SSHing into each host and running
   commands nobody remembers.
2. **There is no single view** of every host's backups.
3. **Config is not saved anywhere else** unless someone remembers to copy it
   after every change.

On top of that, config should be managed from one place: adding and removing
jobs, backends and backup locations on any host, including hooks.

## Goals

- One web UI showing the state of every host's backups, including snapshots.
- Config for every host created and edited in that UI, with history.
- Config stored centrally with secrets encrypted, so "saving the config"
  happens by itself.
- Actions on a host triggered from the UI, starting with initialising a
  repository.
- No inbound access to hosts. Hosts call the central app; nothing connects
  to them.
- Backups keep running when the central app is unreachable.

## Not in this design

- A long-running agent on hosts. See [Deferred](#deferred).
- Sharing one restic repository between hosts. Each host has its own.
- Rootless Docker. Rootless Podman is the priority runtime.

## Overview

There are two parts: the existing `rest-o-matic` CLI on each host, and a new
central app. The CLI gains a check-in step that runs as part of `tick`.

```
host (cron → rest-o-matic tick)                      central app
┌──────────────────────────────┐                    ┌────────────────────────┐
│ 1. check in                  │ ── report ───────▶ │ latest report per host │
│                              │ ◀─ config vN ───── │ config per host,       │
│                              │ ◀─ actions ─────── │   with history         │
│ 2. apply config (if newer)   │                    │ action queue           │
│ 3. run queued actions        │                    │ web UI                 │
│ 4. run due jobs              │                    │                        │
│ 5. report results            │ ── report ───────▶ │                        │
└──────────────────────────────┘                    └────────────────────────┘
        every connection is opened by the host, over HTTPS
```

Three kinds of data move over that connection:

| Flow | Direction | Contents |
|---|---|---|
| Report | host → central | Status of every job, snapshot lists, action results |
| Config | central → host | The config the host should be running |
| Actions | central → host | One-off requests such as "initialise this repository" |

Cron stays the scheduler. `tick` remains a one-shot process, and a host that
is never enrolled behaves exactly as it does today.

## The check-in

A check-in happens at the start of every `tick`. The recommended cron
interval for enrolled hosts is one minute, which bounds how long the UI waits
for a host to notice a change. A tick with nothing due is cheap.

Order within one tick:

1. **Check in.** Send the current report; receive the desired config version
   and any queued actions.
2. **Apply config** if the central version is newer (see
   [Config](#config)).
3. **Run queued actions**, using the config just applied.
4. **Run due jobs**, as today.
5. **Report** results if anything changed.

This order lets "add a backend, then initialise it" complete in a single
tick.

Rules:

- A failed check-in never fails a tick. The host logs a warning and carries
  on with its local config.
- Ticks are independent processes, so a host whose backup takes two hours
  keeps checking in from later ticks and does not look dead.
- The existing per-repository locks apply to actions too. An action against
  a repository that a running job holds stays queued and is reported as
  waiting.
- The check-in format carries a version number, because hosts and the
  central app are upgraded at different times.

## Report

A report is a full picture of the host's current state, not a stream of
events. If the central app misses reports, the next one brings it fully up
to date and nothing needs to be queued or replayed.

Contents:

- Host identity, rest-o-matic version, restic version, time of this tick.
- Config version applied, and a hash of the local config file.
- For each job: last run time, outcome, duration, error text, next due time,
  whether it is running, and the result for each repository.
- For each job and repository: the snapshot list.
- Results of actions run since the last report.

The per-job part of this is what `rest-o-matic status --json` produces. That
command is the first change in the sequence.

### Snapshots are served from a cache

Snapshots change only when rest-o-matic backs up or prunes. After each job,
the host lists that job's snapshots and includes them in its report. The UI
reads snapshot lists from the central store, so viewing them never waits for
a host.

Changes made by running restic directly are not seen until the next refresh.
The host refreshes all lists once a day, and the UI can queue a refresh. The
UI shows an "as of" time on cached data.

### What the UI can show without waiting

| In the UI | Source | Wait |
|---|---|---|
| Last runs, outcomes, errors | Latest report | None |
| Snapshots for any period | Cached list | None |
| Config and its history | Central store | None |
| Host has stopped checking in | Missing reports | None |
| A config edit taking effect | Host applies it | Next check-in |
| Init, run now, other commands | Action queue | Next check-in |

## Config

The central app is the source of truth for each enrolled host's config and
keeps every version. The host keeps a local copy in the existing
`rest-o-matic.yaml` format and always runs from that copy.

Applying a new version:

1. The host receives a version newer than the one it last applied.
2. It validates it with the same checks as `rest-o-matic validate`.
3. If valid, it replaces the local file atomically and keeps the previous
   one. If not, it keeps the current file.
4. It reports "applied" or "rejected" with the reason.

### Drift: when the file is edited on the host

Drift is when the config file on a host no longer matches what the central
app last sent, because someone edited it by hand. The host reports a hash of
its local file on every check-in. If the hash differs from the version it
last applied:

- the host applies nothing from the central app, and
- the UI flags the host and offers two choices: keep the host's version (it
  becomes the new central version) or discard it.

Without this, either the central app would silently overwrite a hand edit, or
the UI would show a config the host is not running.

The same mechanism handles a host that already has a config when it is
enrolled: its local file is adopted as version 1.

### Hooks

Hooks are editable from the central app. A hook is a shell command, so the
central app can run commands as the backup user on every enrolled host. The
central app is therefore fully trusted; see [Trust](#trust).

### Things an editor must handle

- **Renaming a job** strands its old snapshots under the old tag, so they
  are never pruned. The editor should retag them or refuse the rename.
- **Removing a job** leaves its snapshots in the repository with nothing
  pruning them. The UI should say so and not delete anything by itself.
- **Removing a backend** leaves the repository intact.
- **Adding a backend** produces a repository that does not exist yet. The
  host reports it as not initialised and the UI offers to initialise it.

## Backends and templates

Every host has its own repository on every backend it uses. Hosts may have
entirely different backends and policies, though some will match.

A template is a saved backend or policy in the central app that can be added
to several hosts. For example, "offsite S3" holds the endpoint, bucket and
storage credentials. Adding it to a host fills in the parts that differ per
host and copies the result into that host's config:

| Part | Shared or per host |
|---|---|
| Backend type, endpoint, bucket | Shared, from the template |
| Repository path | Per host, derived (for example `.../restic/<host>`) |
| Repository password | Per host, generated |
| Storage credentials | Shared by default; per host is stricter |

It is a copy, not a link. After it is added it belongs to that host, and
editing the template does not change hosts that already use it. This avoids
inheritance rules and stops one edit from changing many hosts at once.

Hosts that share storage credentials can delete each other's repository data
even though they cannot read it. Per-host credentials prevent that, and are
set up at the storage provider.

## Actions

An action is a one-off request queued in the UI and run by the host at its
next check-in.

- **Arbitrary `exec` to begin with.** The UI can queue any command that
  `rest-o-matic exec` accepts. Buttons such as "initialise" and "check" are
  presets for it. The existing safeguards (repository locks and the
  tag-safety gate) apply because it is the same code path. Commands that
  need a terminal, such as `mount`, cannot work. Filtering what may be
  queued can be added later.
- **Run once.** Each action has an ID, and the host records the IDs it has
  run.
- **Expire.** An action not picked up within a time limit is dropped, so a
  host returning after a week does not run stale requests.
- **Report back.** The exit code and a bounded amount of output return in
  the next report.

Initialising is always an explicit action. Doing it automatically would turn
a mistyped URL into a new empty repository that backups then succeed into.

## Secrets

Config contains repository passwords and storage credentials. They are
write-only: the central app stores them but can never read them.

Think of a public key as an open padlock anyone can snap shut, and the
private key as the only key that opens it.

- Each host has its own padlock and key. The key never leaves the host.
- There is one **recovery** padlock and key. The key is kept offline (a
  password manager), never on the central app or any host.
- Every secret is locked twice: one copy for the host that needs it, one
  for recovery.
- The central app stores only locked copies. The UI can replace a secret but
  never display it.
- A generated repository password is created on the host when the
  repository is initialised and uploaded already locked, so its plain text
  never leaves the host.
- A secret typed into the UI is locked in the browser before it is sent.

A standard format is used ([age](https://age-encryption.org) is the intended
choice), so that locked values can be opened with stock tools if
rest-o-matic and the central app are both unavailable.

### On the host

Locked values stay locked in the config file. rest-o-matic unlocks each one
in memory when it runs and never writes the plain text to disk.

```yaml
repositories:
  nas:
    backend: local
    url: /mnt/nas/restic/prd-podman-01
    password: correct-horse-battery-staple        # plain, still valid

  offsite:
    backend: s3
    url: "s3:https://s3.example.com/bucket/restic/prd-podman-01"
    password: !locked "YWdlLWVuY3J5cHRpb24..."    # locked
    env:
      AWS_ACCESS_KEY_ID: AKIA...                  # plain, a choice per value
      AWS_SECRET_ACCESS_KEY: !locked "YWdlLWVu..."
```

- **Plain-text configs keep working.** Locked values are an additional form
  for individual secrets. A host that is not enrolled never has to use them.
  `password_file` and `password_command` are unchanged.
- **The host file and the central copy are identical**, so drift detection
  is a plain comparison and adopting a hand-edited file needs no special
  handling.
- **The host's private key is the one sensitive file**, readable only by the
  backup user. It lives in that user's config directory
  (`~/.config/rest-o-matic/host.key` on Linux), not in the state directory:
  state is disposable and is deleted to fix problems, and a deleted key
  would make every locked value unreadable.
- **Enrolling a host with an existing config rewrites its secrets in
  place**, locking the plain-text values before the file is uploaded. That
  edit must leave comments and YAML anchors intact.
- **Two helper commands** work on the host with the central app down: one
  locks a value to paste into the config, and one reveals a locked value
  using the host's own key, for running plain restic by hand.

### What this does and does not protect

- It protects against the central database, or a backup of it, leaking.
- It does not protect against someone actively controlling the central app,
  who could push a hook that reads the secrets from a host.
- Hook commands are stored in plain text, so anything embedded in a hook
  (a healthcheck URL, a token) is visible in the central app.

### Replacing a dead host

This is the situation backups exist for. It is done on the replacement host,
as part of enrolling it:

1. Enrol the new host and say which host it replaces.
2. It downloads the dead host's config and asks for the recovery key.
3. It unlocks the secrets in memory, locks them again for itself and for
   recovery, and uploads the result.

The recovery key is only ever typed on a machine that is about to hold those
secrets anyway. It never reaches the central app or a browser.

Two things make this dependable:

- **A fallback that needs nothing of ours.** A copy of the config plus the
  recovery key can be opened with the stock `age` tool and used with plain
  restic. This covers losing the central app as well.
- **An offsite backup of the central app's data.** Otherwise a single-site
  loss takes the hosts and the only copy of their locked configs.

### Testing the recovery key

Losing the recovery key is silent until the day it is needed, so the UI lets
its holder prove they still have it:

1. The UI provides a small file: a random code locked for recovery, plus the
   recovery copies of some real secrets. Everything in it is locked.
2. On their own workstation, the holder runs a verify command on that file
   and enters the key.
3. The command prints the code and how many recovery copies it opened.
4. The holder types the code into the UI, which records the date.

The test runs on a workstation, not a backup host, because the recovery key
opens every host's secrets.

| When | Behaviour |
|---|---|
| First setup | Required before any secret is locked |
| About every 90 days | A reminder on the landing page |
| After replacing the recovery key | Required again |

If the test fails while hosts are alive, nothing is lost: a new recovery key
is generated and each host locks its secrets to it at its next check-in.

The test proves the secrets can be recovered, not that backups restore. A
restore drill is a separate, later feature.

## Enrolment

1. The UI creates a host entry and a one-time enrolment token.
2. On the host, an enrol command takes the central URL and the token.
3. The host generates its keypair, registers the public half, and stores a
   per-host credential for future check-ins.
4. The host receives and stores the recovery public key.
5. If the host already has a config, its secrets are locked in place and the
   file is adopted as version 1.

The cron entry stays under the user's control, as it is today.

## Central app

- Lives in its own repository and runs as a container. It holds config
  history and snapshot lists, not backup data, so it needs little storage.
- Built with Blazor Server and MudBlazor, with SQLite for storage. One
  process serves the web UI and the check-in endpoints that hosts call.
- Reached over the tailnet only, behind a login.
- Keeps a change history for config: who changed what and when, with a diff
  shown before a change is sent to a host.
- Flags a host as overdue when its reports stop arriving.
- Must itself be backed up, offsite. That backup contains no readable
  secrets.

### Validating config with the rest-o-matic binary

The editor must check config with the same rules a host uses. The central
app does this by running the released `rest-o-matic` binary, which is
included in its container image:

- It writes the config to a temporary file and runs `rest-o-matic validate`
  with JSON output. The secrets in that file are already locked.
- The image can carry several releases, so a config is validated with the
  version the target host reports. A host on an older release may accept or
  reject different config than the newest one.
- The host remains the final authority. It validates again when it applies
  a config, and reports a rejection.

Two alternatives were rejected. Calling the Go code from C# through a shared
library needs cgo, which ends the simple cross-compilation the release
pipeline uses, and puts two runtimes in one process. Reimplementing the
rules in C# would drift from the host's.

This needs two small additions to the CLI: JSON output for `validate`, and
a JSON Schema for the config file generated from the Go types, which gives
the editor autocomplete and structural checks in the browser.

### Consequences of Blazor Server

- **Secret fields must not be bound to Blazor.** A bound input sends its
  value to the server. A secret is locked in the browser by JavaScript and
  only the locked value is passed on; otherwise the server would see the
  plain text.
- **The check-in format exists in two languages.** Go and C# cannot share
  types, so the format needs a written contract and tests on both sides.
- **No encryption library is needed in the central app.** It never unlocks
  anything: locking happens in the browser and unlocking on hosts.

## User interface

### Landing page: the list of hosts

```
10 hosts    7 healthy    1 overdue    1 failed    1 needs attention      [problems only]

  host             status              jobs           last backup    checked in
✖ pikvm-bk-fourty  Overdue             3              3 days ago     3 days ago
✖ prd-podman-01    1 of 4 failed       gitea: exit 3  2 hours ago    just now
▲ vc4-rwb-01       Not initialised     offsite        yesterday      just now
● ext-backup       Healthy             2              40 min ago     just now
● g5               Healthy             1              5 hours ago    just now
```

A host's status is a roll-up of its jobs and its own condition. Hosts are
grouped by status in this order:

| Status | Meaning |
|---|---|
| Overdue | The host has stopped checking in, or a job is past due and has not run |
| Failed | At least one job's most recent run failed |
| Needs attention | Local config drift, a rejected config, a backend not initialised, a failed action |
| Healthy | Everything ran on schedule and succeeded |

- Problem hosts are always pinned above healthy ones, whatever the sort.
- Within a group, hosts are in alphabetical order. This keeps each host in a
  fixed place.
- Column headers are clickable to sort by another column, such as last
  backup. Sorting reorders hosts within each status group only.
- A summary line shows counts by status, with a "problems only" filter.
- Each row names what is wrong, so a click is only needed to fix it.
- A failure clears once that job next succeeds.

### Host page

Not yet agreed in detail. The working sketch has sections for jobs (with run
history and "run now"), backends (with their initialised state), config
(edit and history) and queued actions, with problems at the top of each.

### Editing config

Config is edited as YAML in an editor (Monaco, which has a built-in diff
view) that validates as you type and shows a diff before sending. This keeps comments and YAML anchors intact, which a
form-based editor would not.

Guided flows are added only where a form does something YAML cannot. The
first is "add a backend": it picks a template, derives the path, generates
the password on the host and queues the initialise action.

## Trust

| If this is compromised | The attacker can |
|---|---|
| Central app, actively controlled | Edit config and hooks, and queue commands, so act as the backup user on every enrolled host |
| Central database or its backup, read only | Read config and hooks; secrets are locked |
| One host | Send false reports about itself, and read its own config and secrets. Nothing about other hosts |
| Network between host and central | Nothing, given HTTPS |

On rootless Podman hosts the backup user is the unprivileged user that runs
the containers, not root.

Compared with a central box holding SSH keys to every host: there are no
keys to steal, no shell, and nothing listening on hosts. The central app is
still a control plane and must be protected as one.

## Behaviour when things fail

| Situation | Result |
|---|---|
| Central app down | Hosts keep backing up from their local config; the next successful check-in brings the central app up to date |
| Host offline | Shown as overdue; queued actions expire |
| Invalid config sent to a host | Host rejects it, keeps the current config, reports why |
| Config edited on the host | Host stops applying central versions until the drift is resolved |
| Action against a busy repository | Stays queued, reported as waiting |
| Host lost | Replacement host re-issues its secrets with the recovery key |

## Delivery sequence

Each step is a separate change and is useful without the ones after it.
Steps 1 to 3 and the host side of 5 and 6 are in this repository. Step 4 is
the central app's repository.

1. **Job status reporting.** Richer run records, a short run history, the
   last tick time and a `status` command with JSON output. No network
   involved. Change: `job-status-reporting`. It builds on a bug fix that
   lands first, `fix-job-concurrency`, which stops a job that is
   still running from being started again.
2. **Locked secret values.** Host keypair, the locked form in config, and
   the commands to lock and reveal a value. Useful on a single host.
   Change: `locked-secrets`.
3. **Enrolment and reporting.** Check-in, report, snapshot lists. The
   check-in carries config versions from the start so later steps do not
   change it.
   This step also adds JSON output to `validate` and the config JSON Schema,
   which the central app's editor needs.
4. **Central app with a read-only dashboard.** Hosts, jobs, snapshots,
   overdue hosts.
5. **Config from the centre.** Editing, apply and reject, drift, templates,
   recovery key setup and test, replacing a host.
6. **Actions.** Queue, `exec`, initialise.

## Deferred

- **Live agent.** An optional long-running process holding an outbound
  connection, so actions run immediately. It would only speed up delivery;
  cron and `tick` would keep doing the scheduling.
- **Browsing files inside a snapshot.** Each folder opened is a round trip
  to the host, which is slow at one check-in per minute.
- **Restore from the UI.** Restoring to a scratch directory fits the action
  queue. Moving files into place needs the container stopped, which ties
  into the planned Quadlet source type. Until then the UI can show the
  exact restore commands for a chosen snapshot.
- **Restore drill.** A periodic check that a recovered password can list
  and restore from a real repository.
- **Filtering queued commands.** Limiting what `exec` commands the UI may
  queue.
- **Signed config.** Hosts applying only config signed by a key that is not
  on the central app, which would remove the central app from the trusted
  set.

## Decision log

### Decided

- Each host runs rest-o-matic and pushes to its own backends.
- Rootless Podman is the priority runtime.
- Hosts call the central app; nothing connects in to hosts.
- The central app can add, change and remove jobs, backends and backup
  locations.
- Hooks are editable from the central app.
- Repositories can be initialised from the UI.
- The UI can queue arbitrary `exec` commands to begin with.
- One repository per host; never shared between hosts.
- Hosts may differ entirely in backends and policies.
- Start without a live agent; cron stays the scheduler.
- The central app lives in its own repository and runs as a container.
- Plain-text configs keep working; locked values are an additional form.
- Config is edited as YAML first, with a guided flow for adding a backend.
- The central app is built with Blazor Server and MudBlazor, with SQLite for
  storage to begin with and Monaco as the config editor.
- The central app validates config by running the `rest-o-matic` binary,
  not through a shared library and not by reimplementing the rules.
- The concurrency defects found on 2026-10-01 are fixed before anything
  else (`fix-job-concurrency`, done). The order in which waiting jobs start
  is left unordered for now.
- The landing page lists hosts: problems pinned first in the order overdue,
  failed, needs attention; healthy hosts alphabetical; clickable column
  headers.
- A locked value is written with a YAML tag: `password: !locked "..."`.
- The host's private key lives in the user's config directory, not the state
  directory.
- A restore drill is a separate, later feature.
- The work is split into separate changes, written one at a time.

### Accepted as proposed

These were recommended and not objected to. They should be confirmed when
the change that implements each one is written.

- Central app is the source of truth for config; hosts run from a local
  copy.
- The drift behaviour described above.
- Snapshot lists cached centrally, refreshed after each job and daily.
- One-minute check-in interval.
- Actions run once and expire.
- Write-only secrets with an offline recovery key, kept locked on the host
  and unlocked in memory.
- Secrets for a replacement host re-issued on that host during enrolment.
- The recovery key test described above.
- Templates added as copies, not links.

### Open

- The layout of the host page and the remaining screens.
- Login and user accounts for the central app.
