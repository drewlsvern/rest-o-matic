## ADDED Requirements

### Requirement: Unknown Keys Are Warned About
A key that rest-o-matic does not read SHALL be reported as a config warning naming the key, where it is and its line, and the nearest key that is read in the same place when one is close in spelling. This SHALL apply at every level of `policies`, `repositories` and `backups`, and to top-level keys other than the config sections and those starting with `x-`, which are kept for YAML anchors. A misspelt key SHALL NOT be treated as the key it resembles: it SHALL be ignored, as before. Keys under a repository's `env` are environment variable names, chosen freely, and SHALL NOT be checked. Where a key is already rejected as an error (under `notify`, a job's `source` and `after` hooks), that SHALL be unchanged.

#### Scenario: Misspelt key
- **WHEN** job `docs` has `hook:` instead of `hooks:`
- **THEN** validation SHALL warn that `hook` under `backups.docs` is not a key rest-o-matic reads, suggesting `hooks` and giving its line

#### Scenario: Anchor holder
- **WHEN** the config has a top-level `x-shared:` key holding anchors
- **THEN** no warning SHALL be given for it

#### Scenario: Warning, not error
- **WHEN** the only problem with a config is an unknown key
- **THEN** `validate` SHALL report it valid, and `tick` SHALL run its jobs and print the warning

#### Scenario: Not corrected
- **WHEN** a job has `nohooks:` holding a `before` list, and no `hooks:`
- **THEN** a warning SHALL be given for `nohooks`, and those commands SHALL NOT be run as the job's hooks

#### Scenario: Env names are free
- **WHEN** a repository has `env: {MY_OWN_VARIABLE: x}`
- **THEN** no warning SHALL be given for the name
