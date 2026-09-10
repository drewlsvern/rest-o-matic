## Why

Existing Restic wrappers (e.g. autorestic) blur "what to back up," "where it goes," and "how/when it runs" into a single flat config block, which gets hard to reason about once retention needs to vary per destination or policies need to be reused across many jobs. rest-o-matic starts from a cleaner separation of concepts and a scheduling model that doesn't require the wrapper to own a daemon or manage the OS scheduler itself. This proposal defines the v1 core wrapper — no container integrations yet — so the foundational config model and execution engine are solid before Podman/Docker support is layered on top later.

## What Changes

- Introduce a config schema built around four separated concepts: **Source** (what to back up — v1 supports `paths:` only), **Repository** (where a restic repository lives — any restic backend: local, s3, sftp, rest-server, b2, azure, etc.), **Policy** (named, reusable schedule + retention rules), and **Hooks** (before/after commands on a job).
- Support named, reusable policies with a three-level override cascade (policy → job-level → per-repository), each level deep-merging key-by-key rather than replacing wholesale — so retention can vary per destination without redeclaring a whole policy.
- Automatically tag every restic snapshot with its job name, and always scope `restic forget` by that tag, so multiple jobs can safely share one repository without one job's retention pruning another's snapshots. User-supplied tags are additive on top of this, never a replacement.
- Add a `tick` command as the only scheduling primitive: rest-o-matic owns no daemon and never writes to the system crontab. The user wires up one generic periodic trigger (cron, launchd, etc.) that repeatedly calls `tick`, and rest-o-matic decides which jobs are actually due by comparing each job's policy schedule against last-run state stored in a local JSON file. Due-checking is calendar-aligned (e.g. `hourly` fires near the top of the hour), and a job that missed its window catches up once, not repeatedly.
- Add a two-tier concurrency model for executing due jobs: jobs targeting the same repository never run concurrently (restic's own repository locking makes this a correctness requirement, not just a performance one), layered under a tunable global `max_concurrent` cap. Jobs that can't run immediately queue and run FIFO.
- Add a `run` execution path: for a due (or manually invoked) job, run `before` hooks, invoke `restic backup` against each configured repository, run `after` hooks, then run `restic forget` per (job, repository) pair using the resolved retention.
- Ship as a single Go binary.

**Out of scope for this change** (explicitly deferred): any container source type (`container`, `quadlet`, `command`), any Podman/Docker integration, any systemd unit generation (reserved for the future Quadlet integration only), and any rest-o-matic-managed crontab writing.

## Capabilities

### New Capabilities
- `config`: The Source/Repository/Policy/Hooks schema, named+reusable policies, and the three-level retention override cascade (policy → job → per-repository), including validation of the config file.
- `backup-execution`: Running a single job end-to-end — before/after hooks, `restic backup` per repository, automatic job-name tagging with additive user tags, and `restic forget` per (job, repository) pair using resolved retention.
- `scheduling`: The `tick` command, calendar-aligned due-detection per policy schedule, catch-up-once behavior after missed windows, and the local JSON state store tracking last-run time/outcome per job.
- `concurrency-control`: The two-tier execution guard (per-repository mutual exclusion, global `max_concurrent` cap) applied to the set of due jobs on a tick, including detecting a job still running from a prior tick so a new tick doesn't start a duplicate.

### Modified Capabilities
(none — greenfield project, no existing specs)

## Impact

- New Go module/binary (`rest-o-matic`) with no prior codebase to migrate.
- New on-disk artifacts: the YAML config file (path TBD in design.md) and a local JSON state file tracking per-job last-run data and in-progress execution markers.
- External dependency: the `restic` binary must be present on the host; rest-o-matic shells out to it rather than reimplementing backend/storage logic.
- No changes to any existing system, since this is the first change in the project.
