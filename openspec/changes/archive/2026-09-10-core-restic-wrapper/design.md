## Context

See proposal.md for motivation. This is a greenfield project (no existing code, no prior config format to migrate). The four spec files (`config`, `backup-execution`, `scheduling`, `concurrency-control`) define the behavioral contracts this design implements. The one contract that most shapes the architecture: **no daemon, no rest-o-matic-managed OS scheduler entries** — `tick` and `run` are one-shot processes, invoked externally, that must exit. That constraint rules out any design relying on a long-lived in-memory scheduler or supervisor process, and pushes coordination (concurrency limits, "is this job already running") into the filesystem, since it's the only thing guaranteed to persist and be visible across separate process invocations.

## Goals / Non-Goals

**Goals:**
- A single Go binary implementing `tick`, `run <job>`, and config validation.
- Correct cross-process coordination (per-repository exclusion, global concurrency cap, duplicate-run prevention) without any daemon or persistent process.
- A config/merge implementation that is a pure, independently testable function separate from I/O and process orchestration.

**Non-Goals:**
- Any container/Podman/Docker source support (deferred; see proposal.md).
- Any systemd unit or crontab generation/management.
- Deep, backend-specific validation of repository connection fields (e.g. verifying S3 credentials are well-formed) — v1 passes them through to `restic` and surfaces restic's own errors.
- Strict cross-process FIFO ordering guarantees (see Risks/Trade-offs).

## Decisions

### Language and core libraries: Go
Per the explore-mode discussion: Go was chosen over Rust, Python, Node/TypeScript, and C#, primarily because the user has no prior experience in either Go or Rust (the two strongest technical fits), and Go's concurrency primitives (goroutines, channels, `sync.Mutex`) map much more directly onto this project's concurrency requirements than Rust's `Arc<Mutex<>>`/`Send`/`Sync` model does for a first-time systems-language user. Secondary factors: Go produces a single static binary (fits the "sits on a server, invoked by cron forever" deployment model), and it's the dominant language in this exact tool niche (restic itself, autorestic).

Expected libraries (confirmed at implementation time, not binding here): a CLI framework (e.g. `cobra`) for the `tick`/`run`/`validate` command surface, a YAML library (e.g. `gopkg.in/yaml.v3`) with a custom `UnmarshalYAML` on the `Source` type to reject unsupported source shapes per the `config` spec, and a small file-locking helper (e.g. `gofrs/flock`) wrapping `flock(2)` rather than hand-rolling syscalls.

### Polymorphic `Source` type via custom unmarshaling
Go has no sum types, so the `config` spec's requirement that only a `paths` source type be accepted (and any other shape rejected) is implemented with a custom `UnmarshalYAML` method on `Source` that inspects which keys are present and errors on anything other than `paths`. This is more manual than Rust's tagged-enum approach would have been, but is a well-trodden pattern in Go YAML/JSON handling and keeps the door open for additional source variants later (each new variant is one more case in the same switch, at the cost of no compiler-enforced exhaustiveness elsewhere in the codebase).

### Config resolution as a pure merge function
The three-level retention cascade (policy → job-level override → per-repository override) is implemented as a pure function: `resolve(policy, jobOverride, repoOverride) -> EffectiveRetention`, performing a key-by-key merge at each step in fixed order. Keeping this free of I/O and CLI concerns means every scenario in the `config` spec can be a direct unit test against this function, independent of file parsing or restic invocation.

### Repository backend handling: pass-through, not reimplementation
A `Repository` config captures a backend type plus a set of backend-specific fields (e.g. `path` for local, `bucket`/`region`/credentials-reference for s3). At invocation time these are translated into the `-r <url>` argument and environment variables restic itself expects (e.g. `RESTIC_REPOSITORY`, `RESTIC_PASSWORD_COMMAND`, backend-specific credential env vars). rest-o-matic does not validate that, say, an s3 repository's credentials are well-formed — restic's own CLI error output is surfaced to the user on failure. This keeps adding new backend support in v1 nearly free (it's restic's problem), matching the `config` spec's requirement that any restic backend be accepted uniformly.

### Scheduling: boundary comparison, not countdown timers
Each named schedule (`hourly`, `daily`, `weekly`) maps to a function that truncates a timestamp down to its calendar boundary (e.g. `hourly` truncates to the start of the current hour). A job is due when `boundary(now) != boundary(last_run)`. This single comparison naturally implements both the calendar-alignment requirement and the catch-up-once requirement from the `scheduling` spec: it doesn't matter how many boundaries were skipped, the next tick simply sees the current boundary differs from the recorded one and runs once, then records the new boundary. All boundary computation uses UTC internally to avoid daylight-saving-time ambiguity (see Risks/Trade-offs).

### Cross-process concurrency via filesystem locks, not a coordinator process
Since `tick` and `run` are independent one-shot processes with no daemon to coordinate through, both concurrency-control requirements and the cross-tick duplicate-run detection requirement are satisfied with plain OS file locks (`flock`), which the kernel automatically releases if a process crashes or is killed — giving self-healing behavior for free, with no PID-liveness bookkeeping needed:

- **Per-repository exclusion**: before running a restic operation against a repository, the process takes a non-blocking exclusive `flock` on a per-repository lock file (e.g. `<state-dir>/locks/<repository>.lock`), held only for the duration of operations against that repository. If the lock can't be acquired, the job is deferred. This correctly serializes any two processes touching the same repository — two ticks, a tick and a manual `run`, or two manual `run`s — without needing to know anything about who holds the lock.
- **Global concurrency cap**: `max_concurrent` pre-created "slot" lock files (e.g. `slot-0.lock` .. `slot-N.lock`) are used the same way — a job execution tries each slot file in turn with a non-blocking `flock`, uses the first one it successfully acquires for its entire execution, and defers if none are free. This gives a working counting semaphore across independent processes without a central coordinator.
- **Duplicate-run detection** falls out of the per-repository lock directly: a job that's still running from a previous tick still holds its repository lock(s), so a new tick's attempt to start the same job against the same repository simply fails to acquire the lock and is deferred — no separate "is job X running" bookkeeping is needed.

Within a single process (one `tick` invocation evaluating several due jobs), dispatch uses an in-process worker pool (goroutines + a buffered channel sized to `max_concurrent`) so FIFO ordering is straightforward to guarantee for jobs evaluated together in that one tick. Jobs deferred because of a lock held by a different process are simply not retried within that tick; the next external trigger's `tick` invocation will re-evaluate and retry them.

### State file: JSON with atomic writes
Per-job last-run time and outcome are stored in one JSON file (schema: a map from job name to `{last_run, last_outcome}`). Writes use the standard write-to-temp-file-then-rename pattern to avoid a partially-written file if the process is killed mid-write, and are protected by an in-process mutex when multiple goroutines within one `tick` might update different jobs' entries concurrently.

## Risks / Trade-offs

- **[Risk]** Strict FIFO ordering is only guaranteed among jobs evaluated within the same `tick` invocation; ordering between independently-invoked processes (e.g. a manual `run` racing a scheduled `tick`) is best-effort, not guaranteed. → **Mitigation**: acceptable for v1 — the consequence is a backup running slightly out of turn, not an incorrect or lost backup. A real cross-process priority queue would require a coordinator process, which conflicts with the no-daemon goal.
- **[Risk]** `flock` is unreliable or unsupported on some network filesystems. If the state/lock directory is placed on such a mount, locking could silently fail to serialize access. → **Mitigation**: document that the state/lock directory must be on a local filesystem; make its location configurable but not defaulted to a network path.
- **[Risk]** UTC-based calendar-boundary truncation avoids most DST ambiguity, but a tick landing exactly on a boundary transition could in rare cases evaluate a job as due slightly early or late relative to the user's local wall clock. → **Mitigation**: accept as a low-severity edge case for v1; boundaries are computed in UTC consistently so behavior is at least deterministic and testable.
- **[Risk]** No backend-specific validation means a misconfigured repository (e.g. a typo'd s3 bucket) is only caught when restic itself fails at backup time, not at config-validation time. → **Mitigation**: acceptable trade-off for v1 simplicity; restic's error output is surfaced directly rather than swallowed.

## Migration Plan

Not applicable — this is the first change in a new project. Initial rollout is: install the binary, write a config file, run `validate`, then wire up one external periodic trigger to call `tick`. No prior data or config format exists to migrate from.

## Open Questions

These are deferrable implementation details that don't change any spec, the chosen approach, or the task breakdown:
- Exact config file discovery path/precedence (e.g. `./rest-o-matic.yaml` vs an XDG config path vs an explicit `--config` flag).
- Exact location of the state/lock directory by default (e.g. XDG state directory vs. next to the config file).
- Logging/output format for `tick` and `run` (plain text vs. structured) — a presentational detail, not a behavioral contract.
