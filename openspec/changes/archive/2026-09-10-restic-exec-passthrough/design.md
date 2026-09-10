## Context

See proposal.md for motivation. This builds directly on code from `core-restic-wrapper`: `internal/lock.AcquireRepository` (the per-repository flock already used by backup/forget), the unexported `repoEnv()` helper in `internal/execution/restic.go` (translates a `config.Repository` into the env vars restic needs), and `config.Config.Backups` (to determine which jobs reference a given repository, for both the tag-safety gate and its error message). The relevant spec is `specs/restic-exec/spec.md` in this change.

## Goals / Non-Goals

**Goals:**
- A `rest-o-matic exec <repository> [--force] -- <restic args>` command living alongside `validate`/`run`/`tick`.
- Reuse existing repository-credential and locking primitives rather than duplicating them.

**Non-Goals:**
- Classifying every restic subcommand exhaustively up front - only a small, explicitly-to-be-verified allowlist of shared-lock commands is needed; everything else defaults safely.
- Retrofitting the `rest-o-matic:` message-prefix convention onto `validate`/`run`/`tick` - those commands don't share a stream with raw, uncaptured restic output the way `exec` does, so there's no ambiguity for them today. Revisiting this for consistency is a separate, later decision.
- Requiring the entire config to pass full cross-job validation before `exec` can run (see Decisions below).

## Decisions

### Exec lives in `internal/execution`, not a new package
Because it needs `repoEnv()`, which is unexported, exec's core logic (building the command, deciding on locking, running the tag-safety gate) lives in a new file within the existing `internal/execution` package rather than exporting that helper or duplicating its logic. The `cmd/rest-o-matic` layer stays a thin wrapper that parses `--force` and the repository name, same pattern as `run`/`tick`.

### Exec only requires the target repository to exist, not the whole config to validate
`run` and `tick` call `loadAndValidate()`, which fails the whole command if *any* job in the config has a problem. For `exec`, that would be a poor fit: it's most useful precisely when something is already wrong (diagnosing a failed job), and an unrelated job's misconfiguration shouldn't block running `restic check` against a perfectly fine repository. Exec therefore only requires that the config file parses, and that the named repository exists in `cfg.Repositories` - full `config.Validate()` is not invoked. This is consistent with the spec, which only requires rejecting an undefined repository reference, not full-config health.

### Lock-type classification: a small, explicitly-provisional allowlist
A package-level set of subcommand names considered shared-lock-only, checked against the first token after `--` (the restic subcommand itself). Starting list (**to be verified against restic's own documented/source behavior during implementation, not assumed**): `snapshots`, `ls`, `find`, `diff`, `stats`, `cat`, `check`, `mount`, `restore`. Anything not in this set - including subcommands added to restic after this list was written - defaults to requiring `lock.AcquireRepository` before running, matching the fail-closed principle from the spec.

### Tag-safety gate: minimal argument inspection, not full restic flag parsing
Exec does not attempt to fully parse restic's own flag grammar. It only needs to answer two narrow questions from the trailing argument list:
1. Is a `--tag` (or `--tag=value`) flag present anywhere in the arguments?
2. For `restore` specifically, is the snapshot-selector argument a relative selector rather than an explicit snapshot ID?

For (2), the initial implementation treats the literal token `latest` as the relative selector to guard on. Restic's actual selector grammar (e.g. host- or tag-qualified forms of "latest") should be checked during implementation; if a broader pattern is needed, this is a narrow, additive change to the detection logic, not a change to the spec's behavior contract (which only requires that an explicit snapshot ID is never gated and a relative selector on a shared repository is).

### Which jobs reference a repository: a small pure helper
A helper resolves, for a given repository name, the list of job names in `cfg.Backups` whose `Repositories` include it. This is used both to decide whether the tag-safety gate applies (more than one job) and to populate the blocked-by-gate message with actual job names.

### Exit code constants
Two reserved constants, distinct from each other and chosen not to collide with restic's own documented exit codes (restic's own exit code table should be checked during implementation before finalizing the exact values - see Open Questions). Used only when restic is never invoked; whenever restic actually runs, `cmd.ProcessState.ExitCode()` is propagated as rest-o-matic's own exit code instead.

### Message prefix
All exec-authored messages (lock-blocked, gate-blocked, undefined-repository) are prefixed with a constant string (e.g. `"rest-o-matic: "`) written to standard error, kept separate from restic's own passed-through output stream in the sense that it's never mixed into a captured buffer - it's simply printed before or instead of invoking restic.

## Risks / Trade-offs

- **[Risk]** The shared-lock allowlist could be wrong or go stale as restic adds subcommands. → **Mitigation**: fail-closed default (unrecognized commands require the lock) means an inaccurate list only ever costs unnecessary waiting, never unsafe concurrency.
- **[Risk]** Detecting `--tag` presence and `restore`'s relative-selector case via lightweight argument inspection (rather than full restic flag parsing) could miss an edge case in restic's own argument grammar. → **Mitigation**: scoped narrowly per spec (only `--tag`/`--tag=` detection and the literal `latest` token for now); a missed edge case only affects the gate's precision, not the locking or passthrough behavior, and can be tightened later without a spec change.
- **[Risk]** Relying on restic's own internal locking as the real backstop when `--force` skips rest-o-matic's flock assumes restic's locking behaves as documented across the versions users run. → **Mitigation**: this mirrors the same assumption `core-restic-wrapper` already makes for `backup`/`forget`; not a new risk introduced by this change.

## Migration Plan

Not applicable - purely additive command, no existing behavior changes.

## Open Questions

- The exact reserved exit code values (distinct from each other and from restic's own documented codes) need restic's current exit code table checked before being finalized - deferred to implementation, does not change the spec or task breakdown.
- Whether restic's `restore` selector grammar has additional relative forms beyond the literal `latest` worth gating on - deferred to implementation; the spec's contract (explicit IDs never gated, relative selectors on shared repos gated) does not change either way.
