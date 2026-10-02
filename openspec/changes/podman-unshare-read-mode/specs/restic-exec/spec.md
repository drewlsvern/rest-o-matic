## ADDED Requirements

### Requirement: Job-Scoped Exec
Exec SHALL accept an optional `--job <name>` option. When it is given, the named job SHALL exist and SHALL back up to the target repository. Otherwise exec SHALL refuse before invoking restic, with a message naming the job and the repository. When the job is valid, restic SHALL be invoked in that job's read mode, whatever the subcommand, so that files restored from a `podman-unshare` job's snapshots get back their original owners. Every other exec behaviour (credentials, output and exit-code passthrough, the concurrency guard, the tag-safety gate) SHALL be unchanged. `--job` SHALL NOT add a tag filter or otherwise change the user's restic arguments.

Without `--job`, exec SHALL invoke restic directly, as before.

#### Scenario: Restore through a podman-unshare job
- **WHEN** a user runs `exec nas --job gitea -- restore <snapshot-id> --target /`, where `gitea` has `read_as: podman-unshare` and backs up to `nas`
- **THEN** restic SHALL run inside the Podman user namespace, and restored files SHALL have the host ownership they had when backed up

#### Scenario: Job not using the repository is refused
- **WHEN** a user runs `exec offsite --job gitea -- snapshots`, and `gitea` does not back up to `offsite`
- **THEN** exec SHALL refuse without invoking restic, naming `gitea` and `offsite`

#### Scenario: Unknown job is refused
- **WHEN** a user runs `exec nas --job nope -- snapshots` and no job `nope` exists
- **THEN** exec SHALL refuse without invoking restic

#### Scenario: Direct job changes nothing
- **WHEN** a user runs `exec nas --job documents -- snapshots`, where `documents` uses read mode `direct`
- **THEN** restic SHALL be invoked exactly as without `--job`

#### Scenario: --job does not satisfy the tag-safety gate
- **WHEN** a user runs `exec nas --job gitea -- forget --keep-daily 7` against a repository shared by several jobs, without `--tag`
- **THEN** exec SHALL still refuse because of the tag-safety gate

### Requirement: Restore Must Match the Job's Read Mode
When exec is given `--job` for a job whose read mode is not `direct`, and the subcommand is `restore`, exec SHALL confirm, before invoking restic, that the snapshot being restored was taken in that read mode, that is, that it carries the tag `restomatic-read=<mode>`. Restoring a snapshot in a different mode from the one it was taken in would give restored files, and the existing parent directories restic restores metadata for, the wrong owners.

- For an explicit snapshot ID (optionally followed by `:<subfolder>`), exec SHALL look up that snapshot's tags and SHALL refuse if the snapshot does not carry the read-mode tag, or if the lookup fails.
- For `latest`, exec SHALL refuse unless every `--tag` filter in the user's arguments includes the read-mode tag, so restic can only select a matching snapshot.
- If exec cannot tell which snapshot is being restored, it SHALL refuse.

A refusal SHALL name the snapshot and the job's read mode, say how to restore instead (without `--job` for a snapshot taken in `direct` mode, or with `--tag restomatic-read=<mode>` for `latest`), and exit with a reserved exit code distinct from exec's other reserved codes and from restic's documented exit codes. `direct` jobs, `exec` without `--job`, and subcommands other than `restore` SHALL NOT be checked.

#### Scenario: Matching snapshot restores
- **WHEN** a user runs `exec nas --job gitea -- restore 9e0e530a --target /home/me/restore`, where `gitea` uses `podman-unshare` and snapshot `9e0e530a` carries `restomatic-read=podman-unshare`
- **THEN** exec SHALL invoke restic inside the Podman user namespace

#### Scenario: Snapshot taken in direct mode is refused
- **WHEN** a user runs `exec nas --job gitea -- restore 1a2b3c4d --target /home/me/restore`, where `gitea` uses `podman-unshare` and snapshot `1a2b3c4d` has no read-mode tag
- **THEN** exec SHALL refuse without invoking the restore, and SHALL say to restore that snapshot without `--job`

#### Scenario: latest without the read-mode tag is refused
- **WHEN** a user runs `exec nas --job gitea -- restore latest --tag gitea --target /home/me/restore`
- **THEN** exec SHALL refuse without invoking restic, and SHALL suggest `--tag gitea,restomatic-read=podman-unshare`

#### Scenario: latest with the read-mode tag restores
- **WHEN** a user runs `exec nas --job gitea -- restore latest --tag gitea,restomatic-read=podman-unshare --target /home/me/restore`
- **THEN** exec SHALL invoke restic inside the Podman user namespace

#### Scenario: Direct jobs and other subcommands are not checked
- **WHEN** a user runs `exec nas --job documents -- restore 1a2b3c4d --target /tmp/r`, where `documents` uses `direct`, or `exec nas --job gitea -- ls 1a2b3c4d`
- **THEN** exec SHALL invoke restic without checking the snapshot's read mode
