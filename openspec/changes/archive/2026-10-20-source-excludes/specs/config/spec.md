## MODIFIED Requirements

### Requirement: Source Declaration
A backup job's `source` SHALL declare what to back up. In this version, only a `paths` source type is supported: a list of one or more filesystem paths. Any other source type SHALL be rejected as invalid at config validation time.

Alongside its type, a `source` MAY declare `exclude`, `exclude_caches` and `one_file_system`, which narrow what is backed up from it (see Source Exclusions). These are not source types: declaring them SHALL NOT be reported as an unsupported source type, and declaring only them, with no `paths`, SHALL be rejected as a source missing its paths.

#### Scenario: Valid paths source accepted
- **WHEN** a job's `source` specifies `paths: [~/documents]`
- **THEN** the config SHALL be accepted and the job's source resolves to that list of paths

#### Scenario: Non-paths source type rejected
- **WHEN** a job's `source` specifies a type other than `paths` (e.g. `container`)
- **THEN** config validation SHALL fail with an error identifying the unsupported source type

#### Scenario: Source missing paths rejected
- **WHEN** a job declares a `paths` source with an empty or missing path list
- **THEN** config validation SHALL fail with an error identifying the job

#### Scenario: Exclusion keys are not source types
- **WHEN** a job's `source` specifies `paths: [~/.local]` and `exclude: [~/.local/share/Trash]`
- **THEN** the config SHALL be accepted with no error or warning about the source

#### Scenario: Exclusions without paths
- **WHEN** a job's `source` specifies `exclude: [Cache]` and no `paths`
- **THEN** config validation SHALL fail with an error identifying the job and stating that `source.paths` must be non-empty

### Requirement: Home Directory in Source Paths
A source path or exclude pattern that is exactly `~`, or that begins with `~` followed by a path separator, SHALL have that leading `~` replaced with the home directory of the user rest-o-matic is running as. The replacement SHALL happen when the config is loaded, so that every command sees the same expanded value. No other part of a path or pattern SHALL be altered: a `~` followed by a user name or any other character, a `~` that is not at the start, and environment variable references SHALL be left exactly as written.

If a source path or exclude pattern needs expanding and the home directory cannot be determined, loading the config SHALL fail with an error identifying the job and the path or pattern.

Expansion SHALL apply only to source paths and exclude patterns. A repository `url` SHALL still be passed to restic exactly as written.

#### Scenario: Path under the home directory
- **WHEN** a job declares `paths: ["~/documents"]` and rest-o-matic runs as a user whose home directory is `/home/me`
- **THEN** the job SHALL back up `/home/me/documents`

#### Scenario: The home directory itself
- **WHEN** a job declares `paths: ["~"]` and the home directory is `/home/me`
- **THEN** the job SHALL back up `/home/me`

#### Scenario: Other tildes are left alone
- **WHEN** a job declares `paths: ["~alice/documents", "/srv/~backup", "/data"]`
- **THEN** all three paths SHALL be used exactly as written

#### Scenario: Home directory unknown
- **WHEN** a job declares `paths: ["~/documents"]` and the home directory cannot be determined
- **THEN** loading the config SHALL fail with an error naming the job and `~/documents`

#### Scenario: Repository url is not expanded
- **WHEN** a repository declares `url: ~/restic-repo`
- **THEN** restic SHALL receive `~/restic-repo` exactly as written

#### Scenario: Exclude pattern under the home directory
- **WHEN** a job declares `exclude: ["~/.local/share/Trash"]` and the home directory is `/home/me`
- **THEN** restic SHALL be given the exclude pattern `/home/me/.local/share/Trash`

#### Scenario: Tilde pattern that is not a home directory
- **WHEN** a job declares `exclude: ["~$*"]` to skip Office lock files
- **THEN** restic SHALL be given the pattern `~$*` exactly as written

## ADDED Requirements

### Requirement: Source Exclusions
A job's `source` MAY declare:

- `exclude`: a list of exclude patterns. Each pattern SHALL be passed to restic unchanged apart from home directory expansion, and SHALL therefore have restic's meaning: a pattern starting with a path separator matches from the root, a pattern without one matches at any depth (so a bare name matches every file or directory with that name), and `**` matches any number of directories. A matching directory and everything under it SHALL be left out of the backup. Omitting `exclude` SHALL be the same as an empty list.
- `exclude_caches`: `true` or `false`, default `false`. When `true`, the contents of any directory containing a valid `CACHEDIR.TAG` file SHALL be left out of the backup.
- `one_file_system`: `true` or `false`, default `false`. When `true`, the backup SHALL NOT descend into a directory under a source path that is on a different filesystem from that source path.

A job that declares none of these SHALL back up exactly as before.

#### Scenario: Absolute exclude
- **WHEN** a job backs up `~/.local` with `exclude: [~/.local/share/containers]`
- **THEN** the snapshot SHALL contain the rest of `~/.local` and nothing under `~/.local/share/containers`

#### Scenario: Bare name matches anywhere
- **WHEN** a job backs up `/srv/code` with `exclude: [node_modules]`, and `node_modules` directories exist at several depths
- **THEN** the snapshot SHALL contain none of them

#### Scenario: Cache directories
- **WHEN** a job has `exclude_caches: true` and a directory under its source contains a valid `CACHEDIR.TAG`
- **THEN** the snapshot SHALL NOT contain that directory's other contents

#### Scenario: Caches kept by default
- **WHEN** a job does not declare `exclude_caches`
- **THEN** directories containing a `CACHEDIR.TAG` SHALL be backed up like any other

#### Scenario: Staying on one filesystem
- **WHEN** a job backs up `/home/me` with `one_file_system: true`, and a network filesystem is mounted at `/home/me/nas`
- **THEN** the snapshot SHALL NOT contain the contents of `/home/me/nas`

#### Scenario: Wrong type
- **WHEN** a job declares `exclude: Trash` (a string, not a list) or `exclude_caches: "yes"`
- **THEN** config validation SHALL fail with an error naming the field and located at its line

### Requirement: Exclude Pattern Checks
Config validation SHALL check each exclude pattern without reading the filesystem:

- A pattern that is not valid glob syntax (for example an unclosed `[`) SHALL be an error identifying the job and the pattern.
- An empty pattern SHALL be an error identifying the job.
- A pattern starting from the root that cannot match anything under any of the job's source paths SHALL be a warning identifying the job and the pattern, since it would exclude nothing.
- A pattern starting from the root that matches one of the job's source paths, or a directory containing one, SHALL be a warning identifying the job, the pattern and the source path, since nothing from that source path would be backed up.

Patterns that do not start from the root SHALL NOT be checked against the source paths.

#### Scenario: Bad glob syntax
- **WHEN** a job declares `exclude: ["/data/[abc"]`
- **THEN** config validation SHALL fail with an error naming the job and `/data/[abc`

#### Scenario: Exclude outside every source path
- **WHEN** a job backs up `/home/me/documents` with `exclude: [/home/me/.cache]`
- **THEN** validation SHALL warn that `/home/me/.cache` is outside every source path of the job and excludes nothing, and the config SHALL still be valid

#### Scenario: Exclude covering a source path
- **WHEN** a job backs up `/home/me/.ssh` and `/home/me/.config` with `exclude: [/home/me]`
- **THEN** validation SHALL warn that the pattern excludes all of `/home/me/.ssh` and `/home/me/.config`

#### Scenario: Wildcard under a source path
- **WHEN** a job backs up `/home/me` with `exclude: ["/home/*/.cache"]`
- **THEN** no warning SHALL be given for the pattern

#### Scenario: Relative patterns are not checked
- **WHEN** a job backs up `/home/me/documents` with `exclude: [Trash]`
- **THEN** no warning SHALL be given for the pattern
