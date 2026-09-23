## Purpose

Defines how rest-o-matic presents its own messages: colouring status labels by severity when a person is watching a terminal, how that is switched on or off, and the guarantee that output to logs, pipes and files stays plain and unchanged.

## ADDED Requirements

### Requirement: Severity Colours on Status Labels
When colour is enabled for a stream, rest-o-matic SHALL colour the status labels it writes to that stream by severity: error labels red, warning labels orange, and success labels green. Only the label or status word SHALL be coloured; the rest of the message SHALL remain uncoloured. The labels and their severities SHALL be:

| Output | Label | Severity |
|---|---|---|
| validate / run / tick config problems | `config error:` | error |
| validate / run / tick config problems | `config warning:` | warning |
| validate | `config is valid` | success |
| job result line | `OK` | success |
| job result line | `FAILED` | error |
| repository result line | `ok` | success |
| repository result line | `deferred` | warning |
| repository result line | `backup failed` / `forget failed` | error |
| tick | `skipping job` | warning |
| tick summary | `<n> succeeded`, when n is non-zero | success |
| tick summary | `<n> failed`, when n is non-zero | error |
| exec's own messages | the `rest-o-matic:` prefix | severity of that message (see below) |
| any command failure | the top-level `Error:` prefix | error |

For exec's own messages, a config error, an undefined repository, and a tag-safety-gate refusal SHALL be errors; a config warning and a lock-blocked refusal SHALL be warnings.

#### Scenario: Config error label coloured red
- **WHEN** `validate` runs with colour enabled on stderr against a config with an error
- **THEN** the `config error:` label SHALL be red and the message text after it SHALL be uncoloured

#### Scenario: Config warning label coloured orange
- **WHEN** `validate` runs with colour enabled on stderr against a config with a warning
- **THEN** the `config warning:` label SHALL be orange

#### Scenario: Valid config coloured green
- **WHEN** `validate` succeeds with colour enabled on stdout
- **THEN** `config is valid` SHALL be green

#### Scenario: Job and repository results coloured by outcome
- **WHEN** `run` completes a job where one repository succeeded and another failed, with colour enabled on stdout
- **THEN** the job's `FAILED` status and the failing repository's `backup failed` SHALL be red, and the succeeding repository's `ok` SHALL be green

#### Scenario: Zero counts in tick summary stay uncoloured
- **WHEN** `tick` prints a summary with `0 failed`, with colour enabled
- **THEN** `0 failed` SHALL NOT be coloured

#### Scenario: Exec lock-blocked prefix coloured as a warning
- **WHEN** exec refuses to run because the repository is in use by another execution, with colour enabled on stderr
- **THEN** its `rest-o-matic:` prefix SHALL be orange

### Requirement: Restic Output Never Coloured
rest-o-matic SHALL NOT add colour to, or otherwise alter, any output produced by restic. This covers exec's passthrough of restic's standard output and standard error, and restic error text embedded in rest-o-matic's own messages.

#### Scenario: Exec passthrough unchanged with colour forced on
- **WHEN** a user runs exec with `--color=always`
- **THEN** restic's standard output SHALL be passed through byte-for-byte identical to running restic directly

#### Scenario: Embedded restic error text uncoloured
- **WHEN** a repository result line reports `backup failed:` followed by restic's error text, with colour enabled
- **THEN** only the `backup failed` label SHALL be coloured and restic's error text SHALL be uncoloured

### Requirement: Colour Enablement
Whether colour is enabled SHALL be decided separately for standard output and standard error, by the first of these rules that applies:
1. `--color=always` enables colour on both streams; `--color=never` disables it on both.
2. If the `NO_COLOR` environment variable is set to a non-empty value, colour is disabled.
3. Otherwise (`--color=auto`, the default), colour is enabled on a stream only if that stream is a terminal.

`--color` SHALL be accepted by every command. A value other than `auto`, `always` or `never` SHALL be rejected with an error before the command does anything else.

#### Scenario: Auto enables colour only on a terminal
- **WHEN** a command runs with the default `--color=auto`, stdout is a terminal, and stderr is redirected to a file
- **THEN** colour SHALL be enabled on stdout and disabled on stderr

#### Scenario: NO_COLOR disables auto colour
- **WHEN** `NO_COLOR=1` is set and a command runs with the default `--color=auto` on a terminal
- **THEN** no colour SHALL be emitted on either stream

#### Scenario: Flag overrides NO_COLOR
- **WHEN** `NO_COLOR=1` is set and a command runs with `--color=always`
- **THEN** colour SHALL be enabled on both streams, even if they are not terminals

#### Scenario: Invalid colour value rejected
- **WHEN** a command runs with `--color=sometimes`
- **THEN** the command SHALL fail with an error naming the accepted values, and SHALL NOT run

### Requirement: Plain Output Unchanged
When colour is disabled on a stream, everything rest-o-matic writes to that stream SHALL be byte-for-byte identical to its output before this capability existed, with no escape sequences.

#### Scenario: Scheduled run output unchanged
- **WHEN** `tick` runs under a scheduler with stdout and stderr redirected to a log, with no `--color` flag
- **THEN** the log SHALL contain no escape sequences and SHALL match the text rest-o-matic produced before colour support

#### Scenario: Never overrides a terminal
- **WHEN** a command runs with `--color=never` on a terminal
- **THEN** no escape sequences SHALL be emitted on either stream
