## Context

- `--state-dir` defaults to `.rest-o-matic`, relative to the working directory ([root.go](../../../cmd/rest-o-matic/root.go)). Locks live in `<state-dir>/locks`, and state in `<state-dir>/state.json`.
- Lock files are created by `gofrs/flock` v0.13 with mode `0600` and are never removed. The `locks/` directory is created with `0755`.
- `state.json` is written through `os.CreateTemp` (mode `0600`) and then renamed into place, so each write leaves it owned by whoever last wrote it.
- `exec` takes the repository lock only for subcommands not on its shared-lock list, and never with `--force` ([exec.go](../../../internal/execution/exec.go)).

See proposal.md for why this matters.

## Goals / Non-Goals

**Goals:**
- A run as the wrong user fails at once, with a clear message, before it creates anything.
- Damage left by earlier runs (root-owned files in a user's state directory) is reported the same way, instead of as a bare `permission denied`.

**Non-Goals:**
- Making mixed-user use work, for example by `chown`ing files back or creating them with looser modes. Under the "one user per host" rule, mixing is always a mistake.
- Checking anything outside the state directory: the config file, restic's cache, or the repository itself.
- Windows ownership (ACLs).

## Decisions

### Refuse, don't repair
Handing files back to the directory's owner (`chown` when running as root) was considered. It would make mixing work, but it would spread root-only behaviour through the lock and state code, and it would need tests running as root. Refusing is about twenty lines, can be tested without root (a fake owner lookup), and pushes people toward the supported setup.

### Compare with the effective uid, via `os.Stat`
Unix builds (`//go:build !windows`) compare `stat.Sys().(*syscall.Stat_t).Uid` with `os.Geteuid()`. The Windows build is a no-op that returns nil. Names for the message come from `os/user.LookupId`, falling back to the numeric uid when a lookup fails (for example a container with no passwd entry).

### What is checked
The state directory itself, `locks/`, every regular file directly in `locks/`, and `state.json`, each only if it exists. Other files (temporary files, anything the user put there) are ignored. The check reads metadata only and never creates anything, so it's safe to run before taking any lock.

### Where it runs
- `run` and `tick`: once, right after the config loads and validates, before the concurrency slot is taken.
- `exec`: inside the same branch that calls `lock.AcquireRepository`, just before it. That keeps the shared-lock and `--force` paths untouched, including `sudo rest-o-matic exec <repo> -- restore …` on rootful Docker hosts. The refusal returns a new `ExitStateDirBlocked` (22), next to `ExitLockBlocked` (20) and `ExitGateBlocked` (21).

### One aggregated error
All mismatched paths are listed in one message, so a user fixing a state directory damaged by a root run sees every file to delete or `chown` at once.

## Risks / Trade-offs

- **[BREAKING] Setups that mix users today start failing.** → Intended. The message gives the exact fix. Called out in the release notes.
- **[Risk] A shared state directory on purpose** (for example a group-writable directory used by two service accounts). → Not supported today either, since `0600` lock files already break it. The message points to separate `--state-dir`s.
- **[Risk] A relative `--state-dir` resolves differently under `sudo`,** depending on the working directory, so the check may pass while looking at a different directory from the usual one. → Unchanged behaviour. The README note recommends an absolute `--state-dir` for scheduled runs.
- **[Trade-off] `exec` shared-lock subcommands run as root skip the check.** They create nothing in the state directory, so there's nothing to protect.

## Migration Plan

Nothing to migrate for correctly configured hosts. A host already damaged by a mixed-user run gets a message listing the root-owned files. Deleting them (they hold no data) or `chown`ing them fixes it.
