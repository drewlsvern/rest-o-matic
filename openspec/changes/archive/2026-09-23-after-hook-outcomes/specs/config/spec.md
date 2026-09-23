## ADDED Requirements

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
