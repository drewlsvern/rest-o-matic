## ADDED Requirements

### Requirement: Locked Repository Credentials
A repository's `password`, and any value under its `env`, SHALL be accepted either as plain text or in the locked form `!locked "<value>"`. Plain-text and locked values MAY be mixed within one config and within one repository. A config that uses no locked values SHALL behave exactly as it did before locked values existed.

The locked form SHALL be accepted only for a repository's `password` and for values under its `env`. Anywhere else in the config it SHALL be an error identifying the place it was found.

Config validation SHALL check that each locked value is well-formed, and SHALL NOT need, read or use any key. The same config SHALL therefore give the same validation result on any machine.

#### Scenario: Plain-text config unchanged
- **WHEN** a config with `password: correct-horse` and plain `env` values is loaded
- **THEN** it SHALL be accepted and SHALL behave as it does today

#### Scenario: Locked and plain values mixed
- **WHEN** a repository has `password: !locked "..."` and `env: {AWS_ACCESS_KEY_ID: AKIA..., AWS_SECRET_ACCESS_KEY: !locked "..."}`
- **THEN** the config SHALL be accepted

#### Scenario: Locked value on another field
- **WHEN** a repository has `url: !locked "..."`
- **THEN** the config SHALL be rejected with an error identifying that field

#### Scenario: Malformed locked value
- **WHEN** a repository has `password: !locked "not a locked value"`
- **THEN** config validation SHALL fail with an error identifying the repository and its password

#### Scenario: Validation without a key
- **WHEN** `validate` is run on a machine with no host key, against a config whose locked values are well-formed
- **THEN** validation SHALL pass
