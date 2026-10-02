## Why

rest-o-matic keeps its lock files and `state.json` in `--state-dir`. Those files are created on first use, never deleted, and created with mode `0600`. If rest-o-matic runs even once as a different user against the same state directory (typically `sudo rest-o-matic run …` while debugging), the files it creates belong to that user. Every later run as the usual user then fails with `permission denied` on a lock file or on `state.json`, until someone finds and deletes them. Scheduled backups stop, and the error doesn't explain why.

This comes up now because the container-volume work (`podman-unshare-read-mode`) will document running rest-o-matic as root on rootful Docker hosts, and as the container user on rootless Podman hosts. That makes "the wrong user touched the state dir" more likely.

## What Changes

- Before `run` or `tick` does anything, and before `exec` takes rest-o-matic's own repository lock, rest-o-matic checks that the state directory, and the lock and state files already in it, belong to the user it's running as. If they don't, it refuses with an error that names the owner and the current user, lists the files that don't match, and says how to fix it: run as the owner, or use a separate `--state-dir`.
- A state directory that doesn't exist yet passes the check, since it will be created by the current user.
- `exec` subcommands that don't take rest-o-matic's lock (such as `snapshots` or `restore`, or anything run with `--force`) create no state files, so they aren't checked. `exec` stays usable as a repair tool from any account, including `sudo rest-o-matic exec … -- restore` on a rootful Docker host.
- `validate` isn't checked, because it never writes to the state directory.
- The check runs on Linux and macOS only. Windows has no Unix owner model, so the check is skipped there.
- There's no override flag. The fix is always to run as the owner, `chown` the directory, or use a separate `--state-dir`.
- **BREAKING** (behavioural): a setup that already mixes users on one state directory, and happens to work because the files were created in a lucky order, will now be refused until it's fixed.

## Capabilities

### New Capabilities
- `state-directory`: who may use the directory holding rest-o-matic's state and lock files, and what happens when the running user doesn't match.

### Modified Capabilities
<!-- none -->

## Impact

- `cmd/rest-o-matic`: `run`, `tick` and `exec` call the check before touching the state directory. `exec` calls it only on the path that acquires the repository lock.
- A new Unix-only ownership check (probably `internal/lock` or a small `internal/statedir` package) with a no-op Windows version.
- README: a short note under state and locking: use one user per state directory, and on rootful Docker hosts run everything as root.
- No config format change.
