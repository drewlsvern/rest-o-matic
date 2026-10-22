## ADDED Requirements

### Requirement: Connection Timeout
A repository MAY declare `connect_timeout`: how long rest-o-matic waits for the repository to be opened before each backup to it (see backup-execution). The config MAY also declare a top-level `connect_timeout`, which applies to every repository that doesn't declare its own. Each SHALL be a duration written as a number followed by `s`, `m` or `h` (such as `30s`, `2m` or `1h`), or `0`. When neither is declared, the timeout SHALL be `2m`. `0` SHALL mean the repository is not checked. A value that is not a valid duration, or a duration below `10s` other than `0`, SHALL be a config validation error identifying the key and where it is.

It SHALL apply to every backend, `local` included.

#### Scenario: Default
- **WHEN** neither the repository nor the top level declares `connect_timeout`
- **THEN** each backup to the repository SHALL be preceded by a check limited to 2 minutes

#### Scenario: Top-level default
- **WHEN** the top level declares `connect_timeout: 30s` and repository `s3` declares none
- **THEN** the check before each backup to `s3` SHALL be limited to 30 seconds

#### Scenario: Repository overrides the top level
- **WHEN** the top level declares `connect_timeout: 30s` and repository `ut-g5` declares `connect_timeout: 5m`
- **THEN** the check before each backup to `ut-g5` SHALL be limited to 5 minutes

#### Scenario: Turned off
- **WHEN** repository `nas` declares `connect_timeout: 0`
- **THEN** backups to `nas` SHALL start without a check

#### Scenario: Invalid value
- **WHEN** repository `s3` declares `connect_timeout: soon` or `connect_timeout: 5s`
- **THEN** config validation SHALL fail with an error identifying `connect_timeout` under repository `s3`
