## ADDED Requirements

### Requirement: Notify Block Declaration
The config MAY contain a top-level `notify` block: a map whose keys are any of `failure`, `recovery` and `success`, each holding a list of commands. Lists MAY be written in either YAML block or inline (`[a, b]`) style. Any other key SHALL be a config error naming the unknown key and its line in the config file. Any other shape, such as a list or a single command string in place of the map, or a single command string in place of a list, SHALL be a config error.

A config with no `notify` block SHALL behave exactly as it did before notifications existed.

#### Scenario: All three keys accepted
- **WHEN** the config declares `notify` with `failure: [alert.sh]`, `recovery: [resolved.sh]` and `success: [ping.sh]`
- **THEN** the config SHALL be accepted with each list assigned to its matching key

#### Scenario: Only some keys
- **WHEN** the config declares `notify` containing only `failure: [alert.sh]`
- **THEN** the config SHALL be accepted, with no `recovery` or `success` commands

#### Scenario: Unknown key rejected
- **WHEN** the config declares `notify` containing the key `failed`
- **THEN** config loading SHALL fail with an error naming the unknown key, its line, and the accepted keys `failure`, `recovery` and `success`

#### Scenario: List in place of the map rejected
- **WHEN** the config declares `notify: [alert.sh]`
- **THEN** config loading SHALL fail with an error

#### Scenario: No notify block
- **WHEN** the config has no `notify` block
- **THEN** the config SHALL be accepted and no notification command SHALL ever run
