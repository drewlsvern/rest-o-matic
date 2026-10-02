## ADDED Requirements

### Requirement: Backup Reads Through the Job's Read Mode
For a job with read mode `podman-unshare`, each `restic backup` SHALL run inside the running user's rootless Podman user namespace, as entered by `podman unshare`, so that files owned by the user's subordinate uids are readable. Each snapshot SHALL record file ownership as seen inside that namespace. The job's hooks and its retention enforcement (`forget`) SHALL NOT be run inside the namespace. Repository credentials and the rest of rest-o-matic's environment SHALL reach restic unchanged. For a job with read mode `direct`, the backup SHALL run exactly as before this requirement.

If `podman` cannot be started, the backup to that repository SHALL fail with an error stating that `podman` is required by the job's read mode.

#### Scenario: Subuid-owned file backed up
- **WHEN** a `podman-unshare` job's source contains a mode `0600` file owned by one of the running user's subordinate uids
- **THEN** the backup SHALL succeed and the snapshot SHALL contain that file

#### Scenario: Same file fails in direct mode
- **WHEN** a `direct` job's source contains the same file
- **THEN** restic SHALL exit with code 3 and the job SHALL be recorded as failed

#### Scenario: Hooks and forget are not wrapped
- **WHEN** a `podman-unshare` job runs its hooks and enforces retention
- **THEN** the hooks and `forget` SHALL run as the running user, outside the Podman user namespace

#### Scenario: podman missing at run time
- **WHEN** a `podman-unshare` job runs on a host without `podman`
- **THEN** each repository's backup SHALL fail with an error stating that `podman` is required by the job's read mode

### Requirement: Read Mode Tag
Every snapshot created by a job whose read mode is not `direct` SHALL carry an additional tag `restomatic-read=<mode>` (for example `restomatic-read=podman-unshare`), alongside the automatic job-name tag and any user tags. Snapshots from `direct` jobs SHALL NOT carry a read-mode tag. The read-mode tag SHALL NOT affect which snapshots retention enforcement selects.

#### Scenario: podman-unshare snapshot tagged
- **WHEN** the `gitea` job with `read_as: podman-unshare` backs up
- **THEN** the snapshot SHALL carry the tags `gitea` and `restomatic-read=podman-unshare`

#### Scenario: direct snapshot not tagged
- **WHEN** the `documents` job with no `read_as` backs up
- **THEN** the snapshot SHALL NOT carry any `restomatic-read=` tag

#### Scenario: Retention unaffected by the read-mode tag
- **WHEN** a job's read mode changes from `direct` to `podman-unshare` and retention is enforced
- **THEN** snapshots from before and after the change SHALL all be considered by that job's retention

### Requirement: Stopping a Wrapped Backup Reaches restic
When rest-o-matic stops a backup that is running in a read mode other than `direct` (because of an interrupt), the stop signal SHALL reach the restic process itself, not only the wrapper that started it, so restic can release its repository lock as it does for a `direct` backup.

#### Scenario: Interrupt during a podman-unshare backup
- **WHEN** rest-o-matic receives SIGTERM while a `podman-unshare` job's backup is running
- **THEN** restic SHALL receive the interrupt and exit, the job's cleanup hooks SHALL run, and the repository SHALL NOT be left with a stale restic lock

### Requirement: Hint for Unreadable Source Files
When a backup fails with restic's exit code 3 and restic's output reports permission errors, the job's failure message SHALL include a hint after restic's own error output. For a `direct` job on Linux with `podman` on `PATH`, the hint SHALL suggest `read_as: podman-unshare`. Otherwise the hint SHALL suggest running rest-o-matic as the files' owner, or as root. For exit codes other than 3, or exit 3 without permission errors, no hint SHALL be added.

#### Scenario: direct job on a Podman host gets the podman-unshare hint
- **WHEN** a `direct` job on Linux, with `podman` on `PATH`, fails with exit code 3 and restic reports `permission denied`
- **THEN** the failure message SHALL include restic's error and a hint suggesting `read_as: podman-unshare`

#### Scenario: Host without podman gets the owner hint
- **WHEN** a `direct` job on a host without `podman` fails with exit code 3 and restic reports `permission denied`
- **THEN** the failure message SHALL suggest running as the files' owner or as root, and SHALL NOT mention `podman-unshare`

#### Scenario: Other failures get no hint
- **WHEN** a backup fails with an exit code other than 3
- **THEN** no read-mode hint SHALL be added
