## Why

Rootless Podman maps container users into the host user's subordinate uid range, so files a container writes to a bind mount can belong to host uids like `100998`. When those files are `0600` or `0700` (a Postgres data directory, TLS keys, secrets), the user running rest-o-matic can't read them. restic then exits with code 3 and the job fails. Whether this happens depends on how each container was started (`--userns=keep-id` or not, one user or several, pod or not), which rest-o-matic can't know. Wrapping restic in `podman unshare` makes every such file readable, whatever the container's setup, and restores then return files to their original owners. No extra privilege is needed.

## What Changes

- New optional job field `read_as`: `direct` (the default, today's behaviour) or `podman-unshare`. With `podman-unshare`, the job's `restic backup` runs as `podman unshare restic …`. Hooks and `forget` are unchanged.
- Snapshots from a `podman-unshare` job get an extra tag, `restomatic-read=podman-unshare`, so it's clear later how the files were read and how they must be restored. `direct` snapshots get no extra tag.
- `exec` gains `--job <job>`: restic then runs in that job's read mode. That's how a `podman-unshare` job's files are restored with the right owners. The job must back up to the target repository.
- When a backup fails with restic's exit code 3 because of permission errors, the job's error adds a hint: on Linux with `podman` installed, a `direct` job is pointed at `read_as: podman-unshare`; otherwise the hint suggests running rest-o-matic as the files' owner, or as root.
- Validation: an unknown `read_as` value is an error. `podman-unshare` is an error on Windows and macOS, with a message explaining that it isn't needed there (Podman runs in a VM, and bind-mounted files are readable directly). On Linux, a warning if `podman` isn't on `PATH`.
- README: a new "Container volumes" section covering bind mounts; rootless Podman with and without keep-id, and when `read_as: podman-unshare` is needed; restoring with `exec --job`; rootful Docker and Podman (run rest-o-matic as root, with a group-restricted `setcap` noted as an advanced option); and rootless Docker (not supported yet).
- Not included: named or anonymous volumes, a `container` source type, stopping or starting containers (left to hooks for now; a later change), `sudo`-based reading, and rootless Docker.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `config`: a new job field `read_as`, with its allowed values, default and platform validation.
- `backup-execution`: the backup runs through the job's read mode, the read-mode tag, stopping a wrapped restic on interrupt, and the permission hint on exit code 3.
- `restic-exec`: a new `--job` option that runs restic in that job's read mode.

## Impact

- `internal/config`: the `ReadAs` field on jobs, and its validation (platform check, podman-on-PATH warning).
- `internal/execution/restic.go`: building the restic command through the job's read mode; process-group cancellation for the wrapped case; the exit-3 hint.
- `internal/execution/job.go`: passes the read mode and the extra tag through to the backup.
- `internal/execution/exec.go`, `cmd/rest-o-matic/exec_cmd.go`: the `--job` flag.
- Tests: unit tests for command construction, tags, validation and the hint. An integration test on Linux CI using real `podman unshare`, if the runner has Podman.
- README and `rest-o-matic.example.yaml`.
- Works well alongside `state-dir-owner-guard`, which should land first: the README advice here (run as the container user or as root, one user per host) relies on it.
