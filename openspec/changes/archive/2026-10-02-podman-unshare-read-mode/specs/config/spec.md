## ADDED Requirements

### Requirement: Job Read Mode
A job MAY declare `read_as`, which sets how restic reads the job's source files. The allowed values SHALL be `direct` and `podman-unshare`. A job that omits `read_as` SHALL behave as `direct`. Any other value SHALL be a config validation error identifying the job and listing the allowed values.

`podman-unshare` SHALL be accepted only on Linux. On any other platform it SHALL be a validation error identifying the job and explaining that it is not needed there, because Podman runs containers in a virtual machine and bind-mounted files are readable directly. On Linux, if `podman` cannot be found on `PATH` when the config is validated, a job using `podman-unshare` SHALL produce a warning identifying the job, and validation SHALL NOT fail on its account.

#### Scenario: Omitted read mode is direct
- **WHEN** a job declares no `read_as`
- **THEN** the config SHALL be accepted and the job's read mode SHALL be `direct`

#### Scenario: podman-unshare accepted on Linux
- **WHEN** a job declares `read_as: podman-unshare` and the config is validated on Linux with `podman` on `PATH`
- **THEN** the config SHALL be accepted without a warning for that job

#### Scenario: Unknown read mode rejected
- **WHEN** a job declares `read_as: sudo`
- **THEN** config validation SHALL fail with an error identifying the job and listing `direct` and `podman-unshare` as the allowed values

#### Scenario: podman-unshare rejected on Windows and macOS
- **WHEN** a job declares `read_as: podman-unshare` and the config is validated on Windows or macOS
- **THEN** config validation SHALL fail with an error identifying the job and stating that the mode is not needed on that platform

#### Scenario: Missing podman warns on Linux
- **WHEN** a job declares `read_as: podman-unshare` and the config is validated on Linux with no `podman` on `PATH`
- **THEN** a warning SHALL be emitted identifying the job, and validation SHALL NOT fail on its account
