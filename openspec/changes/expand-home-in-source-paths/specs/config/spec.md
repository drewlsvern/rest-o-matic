## ADDED Requirements

### Requirement: Home Directory in Source Paths
A source path that is exactly `~`, or that begins with `~` followed by a path separator, SHALL have that leading `~` replaced with the home directory of the user rest-o-matic is running as. The replacement SHALL happen when the config is loaded, so that every command sees the same expanded path. No other part of a path SHALL be altered: a `~` followed by a user name, a `~` that is not at the start of the path, and environment variable references SHALL be left exactly as written.

If a source path needs expanding and the home directory cannot be determined, loading the config SHALL fail with an error identifying the job and the path.

Expansion SHALL apply only to source paths. A repository `url` SHALL still be passed to restic exactly as written.

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
