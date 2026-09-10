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
A `repository` SHALL declare the connection details for a restic repository, including a backend type. Any restic-supported backend type (including but not limited to local, s3, sftp, rest-server, b2, azure) SHALL be accepted; the system SHALL NOT restrict repositories to a specific backend type.

#### Scenario: Local backend repository accepted
- **WHEN** a repository is declared with a local backend and a filesystem path
- **THEN** the config SHALL be accepted

#### Scenario: Non-local backend repository accepted
- **WHEN** a repository is declared with a non-local backend type (e.g. s3) and its required connection fields
- **THEN** the config SHALL be accepted without any special-casing based on backend type

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
