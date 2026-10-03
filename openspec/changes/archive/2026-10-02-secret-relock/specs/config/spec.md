## ADDED Requirements

### Requirement: Marking Env Values as Not Secret
A value under a repository's `env` MAY be written `!plain "<value>"` to record that it is deliberately not secret. Such a value SHALL be used exactly as the same value written without the marker. The marker SHALL be accepted only on values under a repository's `env`; anywhere else it SHALL be a config error identifying the place it was found.

#### Scenario: Marked value is used as written
- **WHEN** a repository has `env: {MY_BUCKET_PREFIX: !plain "host-a/"}`
- **THEN** restic SHALL be started with `MY_BUCKET_PREFIX` set to `host-a/`

#### Scenario: Marker on a password
- **WHEN** a repository has `password: !plain "x"`
- **THEN** the config SHALL be rejected with an error identifying that field
