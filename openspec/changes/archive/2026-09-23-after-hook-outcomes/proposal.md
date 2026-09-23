## Why

A job's `after` hooks run on every outcome, their failures are thrown away, and they can't tell whether the backup worked. So there's no way to alert on a failed backup, to ping a health check on success, or even to notice that a container failed to restart after its backup. `after` also isn't really "always": rest-o-matic has no signal handling, so stopping it with SIGTERM or Ctrl-C (a systemd stop, a timeout) exits immediately and leaves a stopped container down.

## What Changes

- `after:` accepts a map with three keys, each a list of commands:
  - `always`: runs on every outcome, first. This is today's `after` behaviour.
  - `success`: runs after `always` when the job fully succeeded.
  - `failure`: runs after `always` when anything failed.
- The existing list form (`after: [cmd, …]`) keeps working and means `always`. Unknown keys in the map form (e.g. `sucess:`) are config errors.
- Fixed order: `before` → repositories → `always` → `success` or `failure`, whatever order the keys appear in.
- A failing `always` command now counts towards the job outcome, so a failed restart makes the job fail and fires `failure`. **BREAKING** (behavioural): a job whose `after` commands fail was previously reported as succeeding, and is now reported and recorded as failed.
- `always`, `success` and `failure` run every command in their list even if one fails, collecting all errors. `before` still stops at its first failure.
- Every hook receives environment variables: `RESTOMATIC_JOB`, and for `success`/`failure` also `RESTOMATIC_OUTCOME`, `RESTOMATIC_FAILED_REPOS` and `RESTOMATIC_ERROR`.
- On SIGTERM or SIGINT, rest-o-matic stops the in-flight restic or hook commands, starts no new work, then runs `always` and `failure` for each job that was running, within a time limit, before exiting. A second signal skips the cleanup and exits immediately.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `config`: new requirement for the `after` hook's accepted shapes (list or map of `always`/`success`/`failure`) and the rejection of unknown keys.
- `backup-execution`: Job-Level Hook Execution and Before-Hook Failure Aborts Backup change to cover `always`/`success`/`failure` and their order. New requirements cover outcome selection, run-all semantics for post-backup hooks, hook environment variables, and interruption handling.

## Impact

- `internal/config`: `Hooks.After` becomes a struct with its own YAML unmarshalling (the `RepositoryRef` pattern).
- `internal/execution`: `RunJob` ordering and outcome logic, `runHooks` split into stop-on-first-error and run-all variants, environment variables for hooks, context-aware cancellation of restic and hooks.
- `cmd/rest-o-matic`: a signal-aware context for `run` and `tick`, no new job starts after an interrupt, and `printResult` shows after-hook failures.
- Docs: the example config and README hook sections.
- Existing configs parse unchanged. Jobs with failing `after` commands will start being reported as failed.
