# config Specification

## Purpose

Defines the configuration schema for declaring what to back up, where it goes, and how retention is resolved — built around the four separated concepts of Source, Repository, Policy, and Hooks, including the override cascade that lets retention vary per destination without redeclaring a whole policy.

## Requirements

### Requirement: Source Declaration
A backup job's `source` SHALL declare what to back up. In this version, only a `paths` source type is supported: a list of one or more filesystem paths. Any other source type SHALL be rejected as invalid at config validation time.

#### Scenario: Valid paths source accepted
- **WHEN** a job's `source` specifies `paths: [~/documents]`
- **THEN** the config SHALL be accepted and the job's source resolves to that list of paths

#### Scenario: Non-paths source type rejected
- **WHEN** a job's `source` specifies a type other than `paths` (e.g. `container`)
- **THEN** config validation SHALL fail with an error identifying the unsupported source type

#### Scenario: Source missing paths rejected
- **WHEN** a job declares a `paths` source with an empty or missing path list
- **THEN** config validation SHALL fail with an error identifying the job

### Requirement: Repository Declaration
A `repository` SHALL declare the connection details for a restic repository, including a required `backend` field and a `url`. The `url` SHALL be passed to restic exactly as written; the system SHALL NOT add, remove, or rewrite any part of it. Any restic-supported backend (including but not limited to local, s3, sftp, rest, b2, azure) SHALL be accepted; the system SHALL NOT restrict repositories to a specific backend type. A repository that omits `backend` SHALL be a config validation error identifying the repository.

#### Scenario: Local backend repository accepted
- **WHEN** a repository is declared with `backend: local` and an absolute filesystem path as its `url`
- **THEN** the config SHALL be accepted with no errors or warnings for that repository

#### Scenario: Non-local backend repository accepted
- **WHEN** a repository is declared with a non-local backend (e.g. `backend: s3`) and a `url` beginning with that backend's scheme prefix (e.g. `s3:host/bucket`)
- **THEN** the config SHALL be accepted with no errors or warnings for that repository

#### Scenario: Url passed through unmodified
- **WHEN** a repository's config is accepted, with or without warnings
- **THEN** restic SHALL receive the repository's `url` exactly as written in the config

#### Scenario: Missing backend rejected
- **WHEN** a repository is declared without a `backend` field
- **THEN** config validation SHALL fail with an error identifying the repository

### Requirement: Backend and Url Consistency
The system SHALL check each repository's `backend` against the scheme of its `url`. The recognised backends and the `url` scheme prefix each requires SHALL be: `s3` → `s3:`, `sftp` → `sftp:`, `rest` → `rest:`, `swift` → `swift:`, `b2` → `b2:`, `azure` → `azure:`, `gs` → `gs:`, `rclone` → `rclone:`, and `local` → no scheme prefix. `rest-server` SHALL be accepted as an alias for `rest`. A `url` "has a known scheme" when it begins with one of these prefixes. A definite contradiction between `backend` and `url` SHALL be a config validation error identifying the repository and stating both the declared backend and the expected prefix.

#### Scenario: Remote backend with url missing its prefix rejected
- **WHEN** a repository declares `backend: s3` and `url: d6g2.example.com/my-bucket`
- **THEN** config validation SHALL fail with an error identifying the repository and stating that an `s3` backend requires a url beginning with `s3:`

#### Scenario: Remote backend with https url but no prefix rejected
- **WHEN** a repository declares `backend: s3` and `url: https://s3.example.com/my-bucket`
- **THEN** config validation SHALL fail with an error identifying the repository

#### Scenario: Url scheme contradicts backend
- **WHEN** a repository declares `backend: s3` and `url: sftp:user@host:/srv/restic`
- **THEN** config validation SHALL fail with an error identifying the repository, the declared backend, and the scheme found in the url

#### Scenario: Local backend with remote url rejected
- **WHEN** a repository declares `backend: local` and `url: s3:host/bucket`
- **THEN** config validation SHALL fail with an error identifying the repository

#### Scenario: rest-server alias accepted
- **WHEN** a repository declares `backend: rest-server` and `url: rest:https://host:8000/`
- **THEN** the config SHALL be accepted with no errors or warnings for that repository

#### Scenario: Consistency errors reported with other validation errors
- **WHEN** a config has both a backend/url contradiction and an unrelated validation error (e.g. a job referencing an undefined policy)
- **THEN** config validation SHALL report both errors, not only the first found

### Requirement: Repository Configuration Warnings
The system SHALL emit a warning, and SHALL NOT fail validation, for repository configurations that are likely but not certainly wrong. Warnings SHALL be written to standard error, SHALL identify the repository, and SHALL be distinguishable from errors. The following SHALL produce a warning:
- a `backend` value that is not one of the recognised backends or aliases, in which case the `url` SHALL be used unchanged and no consistency check SHALL be applied; and
- `backend: local` with a `url` that is a relative filesystem path.

#### Scenario: Unrecognised backend warns and proceeds
- **WHEN** a repository declares `backend: s33` and `url: s3:host/bucket`
- **THEN** a warning SHALL be emitted identifying the repository and the unrecognised backend, validation SHALL NOT fail on its account, and restic SHALL receive the url unchanged

#### Scenario: Relative local path warns and proceeds
- **WHEN** a repository declares `backend: local` and `url: backups/restic`
- **THEN** a warning SHALL be emitted identifying the repository and stating that the path is resolved relative to the current working directory, and validation SHALL NOT fail on its account

#### Scenario: Warnings do not change a valid result
- **WHEN** a config's only findings are warnings
- **THEN** the validate command SHALL print the warnings and still report the config as valid, and run and tick SHALL proceed

### Requirement: Named Policy Definition
Policies SHALL be defined once, by name, in a top-level policies collection, each specifying a schedule and a retention rule set. A policy SHALL be usable by more than one backup job.

#### Scenario: Policy reused across multiple jobs
- **WHEN** two backup jobs each reference the same named policy
- **THEN** both jobs SHALL resolve the same schedule and retention rules from that one policy definition

### Requirement: Job Policy Reference and Resolution
A backup job SHALL reference a named policy to obtain its baseline schedule and retention. Referencing a policy name that is not defined SHALL be a config validation error.

#### Scenario: Job resolves schedule and retention from its policy
- **WHEN** a job references a defined policy and specifies no overrides
- **THEN** the job's effective schedule and retention SHALL equal the referenced policy's schedule and retention

#### Scenario: Undefined policy reference rejected
- **WHEN** a job references a policy name that does not exist in the policies collection
- **THEN** config validation SHALL fail with an error identifying the job and the missing policy name

### Requirement: Job-Level Retention Override Merge
A backup job MAY specify a partial `retention` block that overrides its referenced policy's retention. The override SHALL be applied as a key-by-key merge on top of the policy's retention: keys present in the job override SHALL replace the corresponding policy value, and keys absent from the override SHALL retain the policy's value.

#### Scenario: Partial override keeps unspecified keys from the policy
- **WHEN** a job's policy defines `retention: {hourly: 24, daily: 30, weekly: 12}` and the job overrides with `retention: {weekly: 8}`
- **THEN** the job's effective retention SHALL be `{hourly: 24, daily: 30, weekly: 8}`

#### Scenario: No job-level override leaves policy retention unchanged
- **WHEN** a job specifies no `retention` override
- **THEN** the job's effective retention SHALL equal the referenced policy's retention exactly

### Requirement: Per-Repository Retention Override Merge
Within a job's list of repositories, an individual repository entry MAY specify its own partial `retention` block. This override SHALL be applied as a key-by-key merge on top of the job's already-resolved effective retention (policy merged with any job-level override), producing a distinct effective retention for that (job, repository) pair.

#### Scenario: Repository override wins over job-level and policy values
- **WHEN** a job's effective retention (after policy and job-level merge) is `{hourly: 24, daily: 30, weekly: 8}` and one of its repositories overrides with `retention: {daily: 30}`
- **THEN** that repository's effective retention for this job SHALL be `{hourly: 24, daily: 30, weekly: 8}` while any other repository in the same job without an override SHALL retain `{hourly: 24, daily: 30, weekly: 8}` from the job level

#### Scenario: Repositories in the same job can have different effective retention
- **WHEN** a job lists two repositories and only one of them specifies a per-repository retention override
- **THEN** each repository's effective retention SHALL be resolved independently, and the two repositories MAY end up with different effective retention values

### Requirement: Repository Reference Validation
Each entry in a job's repositories list SHALL reference a repository defined in the top-level repositories collection. Referencing an undefined repository SHALL be a config validation error.

#### Scenario: Undefined repository reference rejected
- **WHEN** a job's repositories list includes a name not present in the repositories collection
- **THEN** config validation SHALL fail with an error identifying the job and the missing repository name

### Requirement: After Hook Declaration
A job's `after` hook SHALL accept either of two shapes:
- a list of commands, which SHALL be treated exactly as an `always` list; or
- a map whose keys are any of `always`, `success` and `failure`, each holding a list of commands.

Lists MAY be written in either YAML block or inline (`[a, b]`) style. In the map form, any key other than `always`, `success` or `failure` SHALL be a config error naming the unknown key and its line in the config file. Any other shape, such as a single command string, SHALL be a config error.

#### Scenario: List form treated as always
- **WHEN** a job declares `after: [start.sh]`
- **THEN** the config SHALL be accepted and `start.sh` SHALL be the job's only `always` command, with no `success` or `failure` commands

#### Scenario: Map form accepted
- **WHEN** a job declares `after` as a map with `always: [start.sh]`, `success: [ping.sh]` and `failure: [alert.sh]`
- **THEN** the config SHALL be accepted with each list assigned to its matching key

#### Scenario: Map form with only some keys
- **WHEN** a job declares `after` as a map containing only `failure: [alert.sh]`
- **THEN** the config SHALL be accepted, with empty `always` and `success` lists

#### Scenario: Unknown key rejected
- **WHEN** a job declares `after` as a map containing the key `sucess`
- **THEN** config loading SHALL fail with an error naming the unknown key, its line, and the accepted keys `always`, `success` and `failure`

#### Scenario: Bare string rejected
- **WHEN** a job declares `after: start.sh` (a string, not a list or map)
- **THEN** config loading SHALL fail with an error
