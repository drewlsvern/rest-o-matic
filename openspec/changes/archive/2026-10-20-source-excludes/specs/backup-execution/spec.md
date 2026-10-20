## ADDED Requirements

### Requirement: Backup Applies the Source's Exclusions
Each `restic backup` a job runs SHALL be given the job's exclusions: one `--exclude` per pattern in `source.exclude`, in the order the patterns are written and after home directory expansion; `--exclude-caches` when `source.exclude_caches` is `true`; and `--one-file-system` when `source.one_file_system` is `true`. The same exclusions SHALL be applied to every repository the job backs up to, and in every read mode. A job with no exclusions SHALL run restic with the same arguments as before this requirement.

Exclusions SHALL NOT affect which snapshots retention enforcement selects.

#### Scenario: Patterns in order
- **WHEN** a job declares `exclude: [~/.local/share/containers, Trash]` and the home directory is `/home/me`
- **THEN** each `restic backup` for the job SHALL be given `--exclude /home/me/.local/share/containers --exclude Trash`, in that order

#### Scenario: Flags only when set
- **WHEN** a job declares `exclude_caches: true` and does not declare `one_file_system`
- **THEN** `restic backup` SHALL be given `--exclude-caches` and SHALL NOT be given `--one-file-system`

#### Scenario: Every repository
- **WHEN** a job with exclusions backs up to two repositories
- **THEN** both backups SHALL be given the same exclusions

#### Scenario: Wrapped read mode
- **WHEN** a `podman-unshare` job declares exclusions
- **THEN** the backup run inside the Podman user namespace SHALL be given them

#### Scenario: Changing exclusions keeps the parent snapshot
- **WHEN** a job that has backed up before adds an exclude pattern and runs again
- **THEN** restic SHALL use the job's previous snapshot as its parent, and the run SHALL NOT need to read unchanged files again
