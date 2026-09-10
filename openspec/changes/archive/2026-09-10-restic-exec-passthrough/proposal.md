## Why

rest-o-matic currently only lets a user run restic through two narrow paths: `backup`/`forget`, both invoked indirectly via `tick`/`run`. Anything else restic can do — `snapshots`, `check`, `restore`, `mount`, `diff`, `dump` — requires the user to hand-type `-r <url>` and export credential env vars themselves, even though that connection info already lives in rest-o-matic's own config. Autorestic solves this with an `exec` passthrough command; rest-o-matic needs the equivalent, but must account for a hazard autorestic's model doesn't emphasize as sharply: rest-o-matic explicitly allows one repository to be shared by multiple jobs (each snapshot auto-tagged by job name specifically so `forget` stays safe per job), so a naive raw passthrough could easily undo that safety property.

## What Changes

- Add an `exec <repository> [--force] -- <restic subcommand and args>` command that injects a configured repository's `-r` URL and credential env vars, then passes the remaining arguments straight to the real `restic` binary.
- Passthrough is transparent: stdin/stdout/stderr connect directly to the restic subprocess (no capturing or reinterpreting, unlike the existing `Backup`/`Forget` functions), and restic's own exit code is propagated untouched whenever restic actually runs.
- Locking for `exec` mirrors restic's own internal shared/exclusive lock distinction rather than an invented "dangerous command" list: a short allowlist of subcommands known to take only a shared lock skip rest-o-matic's own per-repository flock; everything else, including any subcommand rest-o-matic doesn't recognize, defaults to requiring that flock (fail-closed).
- `--force` overrides only that lock-wait behavior (skips rest-o-matic's own flock, relies on restic's own internal locking as the backstop) — it does not affect the tag-safety gate below.
- Add a tag-safety gate, independent of locking: `forget` and `tag` are refused against a repository referenced by more than one job unless the user's own arguments already include `--tag`; `restore` is gated the same way only when using a relative snapshot selector (`latest`) rather than an explicit snapshot ID; `prune` is exempt (no tag concept applies to it). This gate has no override flag — the only ways around it are supplying `--tag` directly, or invoking `restic` outside of `exec`.
- All of rest-o-matic's own output (as opposed to restic's passed-through output) is prefixed consistently (e.g. `rest-o-matic: ...`) so it's distinguishable from restic's own messages sharing the same stream.
- Reserve a small range of rest-o-matic-specific exit codes (distinct from restic's own, and from each other) for "blocked by lock" (retryable) versus "blocked by the tag-safety gate" (not retryable by waiting) — never colliding with a restic exit code, since restic's own code is propagated as-is whenever restic actually runs.

**Explicitly out of scope for this change:**
- Enriching the "blocked by lock" message with which job/process holds it (would need a metadata sidecar on lock acquisition) — noted as a future enhancement.
- Any automatic/smart flag injection (e.g. auto-appending `--tag` on the user's behalf) — deliberately rejected in favor of a hard gate with no injection.
- Any change to the existing config, backup-execution, scheduling, or concurrency-control behavior from the `core-restic-wrapper` change — this is purely additive.

## Capabilities

### New Capabilities
- `restic-exec`: A passthrough command for running arbitrary restic subcommands against a configured repository, with locking that mirrors restic's own shared/exclusive lock model and a tag-safety gate that prevents unscoped destructive operations against a repository shared by multiple jobs.

### Modified Capabilities
(none — this change does not alter the behavior of any existing capability)

## Impact

- New CLI command (`exec`) in `cmd/rest-o-matic`, reusing the existing `internal/lock.AcquireRepository` flock and the existing `internal/config.Repository` type / credential-to-env-var translation already used by `backup`/`forget`.
- No changes to the config schema itself, and no changes to any other command's behavior.
- New reserved CLI exit codes for this command specifically; no change to existing commands' exit code conventions.
