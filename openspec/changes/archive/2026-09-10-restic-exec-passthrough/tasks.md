## 1. Config Helper

- [x] 1.1 Add a helper that, given a repository name, returns the names of every job in the config that references it (used by both the tag-safety gate and its error message)
- [x] 1.2 Unit tests: a repository referenced by zero, one, and multiple jobs all return the expected job name lists

## 2. Restic Exit Code Reference

- [x] 2.1 Check restic's current documented exit codes and choose two reserved rest-o-matic exit code constants (lock-blocked, gate-blocked) that don't collide with them or with each other
- [x] 2.2 Document the chosen values and their meaning alongside the constants

## 3. Lock-Type Classification

- [x] 3.1 Verify, against restic's own documentation/source, which subcommands take only a shared (non-exclusive) lock; adjust the illustrative starting list from design.md as needed
- [x] 3.2 Implement a lookup that classifies a subcommand name as shared-lock-only or (default) requiring the exclusive per-repository guard
- [x] 3.3 Unit tests: each listed shared-lock subcommand classifies as such; an arbitrary/unrecognized subcommand name defaults to requiring the guard

## 4. Tag-Safety Gate

- [x] 4.1 Implement detection of a `--tag`/`--tag=value` flag anywhere in a trailing argument list
- [x] 4.2 Verify restic's actual selector grammar for `restore` and implement detection of a relative snapshot selector (starting with the literal `latest`) versus an explicit snapshot ID
- [x] 4.3 Implement the gate itself: refuse `forget`/`tag` without a tag filter when the repository is referenced by more than one job; refuse `restore` the same way only when the selector is relative; exempt `prune` entirely; no effect when the repository is referenced by zero or one job
- [x] 4.4 Confirm no command-line option (including `--force`) affects this gate
- [x] 4.5 Unit tests covering every Scenario in `specs/restic-exec/spec.md` related to the tag-safety gate

## 5. Concurrency Guard Integration

- [x] 5.1 Wire the lock-type classification from Section 3 to `internal/lock.AcquireRepository`: shared-lock subcommands skip it, everything else acquires it before invoking restic and releases it after
- [x] 5.2 Implement the `--force` option to skip the guard acquisition entirely for that invocation, independent of subcommand classification
- [x] 5.3 When the guard cannot be acquired (and force was not given), refuse to invoke restic and report the lock-blocked message and exit code
- [x] 5.4 Unit/integration tests covering every Scenario in `specs/restic-exec/spec.md` related to locking and force, including simulating a held lock from a separate process

## 6. Transparent Passthrough Execution

- [x] 6.1 Implement the restic invocation itself: inject `-r <url>` and credential environment variables for the target repository (reusing the existing repository-to-env translation), connect stdin/stdout/stderr directly to the restic subprocess, and pass the user's trailing arguments through unmodified
- [x] 6.2 Propagate restic's real exit code as rest-o-matic's own exit code whenever restic is actually invoked
- [x] 6.3 Integration test against a real local-backend restic repository: run a read-only subcommand (e.g. `snapshots`) through exec and confirm output and exit code match calling restic directly

## 7. Messaging

- [x] 7.1 Define and apply a consistent prefix for every message exec produces itself, distinguishing it from restic's own passed-through output
- [x] 7.2 Implement the lock-blocked message: names the repository, states it's in use by another execution, mentions the force option
- [x] 7.3 Implement the gate-blocked message: lists the actual job names referencing the repository, states plainly that no option bypasses this check
- [x] 7.4 Implement the undefined-repository message, produced before any restic invocation is attempted

## 8. CLI Wiring

- [x] 8.1 Add the `exec <repository> [--force] -- <restic args>` command to `cmd/rest-o-matic`, loading the config (parse only, not full cross-job validation) and resolving the target repository
- [x] 8.2 Reject an undefined repository name before attempting anything else
- [x] 8.3 Integration tests covering the full command: undefined repository, lock-blocked, gate-blocked, force-bypasses-lock-not-gate, and a successful passthrough end to end

## 9. Documentation

- [x] 9.1 Document the `exec` command in the README: invocation shape, the locking model (mirrors restic's own shared/exclusive split, `--force` scoped to locking only), the tag-safety gate and why it has no bypass, and the reserved exit codes
- [x] 9.2 Note in the README that which job/process holds a lock is not surfaced yet (future enhancement)
