## Context

- `ResticRunner.Backup` runs `exec.CommandContext(ctx, r.binary(), "-r", url, "backup", paths…, --tag…, --json)` with `repoEnv(repo)`, which is the caller's environment plus the password and credential variables ([restic.go](../../../internal/execution/restic.go)). Every non-zero exit, including 3, is returned as an error that includes restic's stderr.
- Cancellation (`configureResticCancel`) sends SIGINT to `cmd.Process` and kills it after `resticWaitDelay` (10 s). restic isn't placed in its own process group.
- `runJob` builds the tags as `[jobName] + job.Tags` and backs up to each repository in turn. `forget` is scoped with `--tag <jobName>`.
- `exec` runs restic through `PassThrough` with stdin, stdout and stderr connected, and no signal handling of its own.
- `podman unshare` runs a command inside the user's rootless user namespace. That namespace is set up from `/etc/subuid` and `/etc/subgid`: the user maps to 0, and subordinate uid `start+k` maps to `k+1`. It's the same namespace whatever `--userns` each container uses.

See proposal.md for the motivation and specs/ for the behaviour.

## Goals / Non-Goals

**Goals:**
- A rootless Podman user can back up any bind-mounted data without extra privilege, whatever each container's userns setup.
- Restores of that data return files to their original host owners.
- A user who hits the permission problem is told about `read_as`.

**Non-Goals:**
- Choosing the read mode automatically, either at backup time (from file ownership) or at restore time (from the snapshot's tag).
- Guaranteeing `restic mount` under `podman unshare`. FUSE inside a user namespace depends on the host. `restore`, `dump` and `ls` are the supported ways to restore.
- `sudo` or other privilege-raising modes, rootless Docker, named volumes, and stopping or starting containers.

## Decisions

### A named mode, not a free-form command prefix
`read_as: podman-unshare` rather than something like `restic_wrapper: ["podman", "unshare"]`. A named mode can be validated per platform, can drive the right hint and tag, and is documented in one place. A free-form prefix could be anything, so rest-o-matic couldn't reason about restores, signals or messages. New modes can be added to the enum later.

### Building the command
In `podman-unshare` mode the command is `podman unshare <restic> <args…>`, where `<restic>` is resolved to an absolute path with `exec.LookPath` first. That gives a clear "restic not found" error before podman runs, and it doesn't depend on `PATH` inside the namespace. The environment is `repoEnv(repo)`, set on the `podman` process; `podman unshare` passes its environment to the command (to be checked by the integration test). `podman` itself is found by `exec.LookPath` at run time. If it's missing, the error names the job's read mode. A single helper builds the `*exec.Cmd` for a given read mode and is shared by `Backup` and `PassThrough`, so backup and `exec --job` can't drift apart.

### Stopping a wrapped restic: signal the process group
It isn't documented whether `podman unshare` forwards SIGINT to its child. So we don't rely on it: in any wrapped mode, the backup command starts in its own process group (`Setpgid`), and `Cancel` sends SIGINT to the whole group (`-pgid`). `WaitDelay` still kills the group if anything remains. Both restic and the podman wrapper get the signal directly. `direct` mode keeps today's behaviour unchanged. This lives in the Unix restic file. The Windows build never takes this path, since validation rejects the mode there.

`exec --job` doesn't use a separate process group. It runs in the foreground in a terminal, where Ctrl-C already reaches the whole foreground process group, restic included. Putting restic in its own group would stop Ctrl-C from reaching it at all.

Alternative rejected: signalling only `podman` and trusting it to forward the signal. If it doesn't, restic is killed after 10 s and leaves a stale repository lock, which is exactly what the SIGINT design avoids.

### The tag: `restomatic-read=podman-unshare`, only for non-`direct` modes
`direct` snapshots stay untagged, so every existing snapshot is correctly understood as `direct` without rewriting anything. Restic tags can hold `=` (only commas are special). `forget --tag <job>` matches snapshots that carry the job tag among others, and restic's default `--group-by host,paths` ignores tags, so retention treats a job's snapshots as one set before and after the mode changes.

### The exit-3 hint
When `cmd.Run` returns an `*exec.ExitError` with `ExitCode() == 3` and restic's stderr contains `permission denied` (case-insensitive), a hint line is added after the existing error. Which hint is chosen at failure time: `direct` on Linux with `podman` found by `LookPath` gets the `podman-unshare` hint. Anything else, including a `podman-unshare` job that still can't read a file (for example one owned by a uid outside the user's range), gets the owner-or-root hint.

### `exec --job`
Checking uses only the named job and its repository references: the job must exist and list the target repository. That follows exec's rule that problems elsewhere in the config don't block it. The job's read mode applies to every subcommand, which keeps things simple: `snapshots`, `ls`, `restore` and `dump` all run in the same namespace the data was read in. The tag-safety gate and the concurrency guard run unchanged, before restic is started.

### A wrapped restore must match the snapshot's read mode
Found while testing: restoring an untagged (`direct`) snapshot under `podman unshare` to `--target /` changed the owner of the *existing parent directories* of the restored file, not just the file. restic restores metadata for every directory on the path, and a uid recorded as 1000 means a subordinate uid inside the namespace. So `exec --job` for a wrapped job refuses a `restore` whose snapshot doesn't carry the job's `restomatic-read=` tag (see the restic-exec spec).

- The snapshot argument is the one positional argument after `restore`, found by skipping flags and the values of restic's known value-taking flags. If no single positional is found, the restore is refused, which fails closed if restic adds a new value flag.
- An explicit ID (with an optional `:subfolder`) is looked up with a direct `restic snapshots --json <id>`, which only reads.
- For `latest`, rest-o-matic doesn't reimplement restic's selection. Instead it requires every `--tag` filter to include the read-mode tag, since restic ORs separate `--tag` flags and ANDs the comma-separated tags within one.
- Only `restore` is checked: `dump`, `ls` and `mount` read snapshots but don't set owners on the host. Only wrapped modes are checked: a direct restore run by a normal user can't give files to other uids.
- A refusal exits with the reserved code 23.

The README documents restoring into a separate `--target` and moving files into place with `podman unshare mv`, which never touches the real parent directories. `--target /` also reports harmless errors under `podman unshare`, because `/` and `/home` belong to the real root.

### Platform validation
`runtime.GOOS != "linux"` with `podman-unshare` is a validation error explaining that the mode isn't needed there. A missing `podman` on Linux is only a warning, since the config may be validated on a different machine from the one that runs it.

## Risks / Trade-offs

- **[Risk] `podman unshare` behaves differently than expected** in exit-code passthrough (needed for the exit-3 hint and for exec), environment passthrough, or signals. → An integration test covers all three on Linux CI with real Podman (`ubuntu-latest` is expected to include it; if not, it gets installed in the job). The test is skipped where `podman` is missing.
- **[Risk] Restoring a snapshot in a different mode from the one it was taken in** gives the files and their existing parent directories wrong owners. → `exec --job` refuses mismatched restores (see Decisions). `exec` without `--job` isn't checked; a direct restore run by a normal user can't give files to other uids.
- **[Risk] Files owned by uids outside the user's mapping** (another host user's files) appear as `nobody` inside the namespace and stay unreadable. → restic exits 3 and the owner-or-root hint is shown. That's working as intended.
- **[Trade-off] The first backup after switching to `podman-unshare` records different ownership for every file,** so restic writes new metadata. File contents are deduplicated, so the repository grows only by metadata.
- **[Trade-off] `restic mount` may not work under `--job`.** → Documented. `restore`, `dump` and `ls` are the supported paths.

## Migration Plan

Opt-in and non-breaking: existing jobs are `direct` and behave exactly as before. To adopt it, add `read_as: podman-unshare` to the affected jobs and run once by hand. Rollback means removing the field. Snapshots taken in the meantime keep their tag, and are restored with `exec --job` only while the job is still in that mode (see the risk above).
