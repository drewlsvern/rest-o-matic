## Purpose

Lets a person, or a program such as the central app's editor, check a config file by the same rules rest-o-matic applies when it runs, and find each problem in the file.

## ADDED Requirements

### Requirement: Problems Are Located in the File
Every problem `validate` reports SHALL carry, where it can be determined, the dotted path of the field concerned (such as `backups.docs.policy`) and the line and column in the config file. When the field itself is absent (a required key that is missing), the location SHALL be that of the nearest enclosing key that is present. A value reached through a YAML alias or merge key SHALL be located where it is used, not where the anchor is defined. The text output SHALL begin each located problem with `line N:`.

#### Scenario: Undefined policy
- **WHEN** job `docs` on line 8 has `policy: hott` and no such policy exists
- **THEN** the problem SHALL have path `backups.docs.policy`, line 8, and the column of `hott`

#### Scenario: Missing key
- **WHEN** job `docs`, whose key is on line 6, has no `policy`
- **THEN** the problem SHALL have path `backups.docs.policy` and the line and column of the `docs` key

#### Scenario: Located through an alias
- **WHEN** repository `nas` on line 12 takes its `url` from an alias, and that url doesn't match its backend
- **THEN** the problem SHALL be located at the alias on line 12

### Requirement: JSON Output
`rest-o-matic validate --json` SHALL print exactly one JSON document to standard output, whatever the outcome, containing a format version, the version of rest-o-matic, whether the config is valid, and every problem found. Each problem SHALL have a severity (`error` or `warning`), a message, and the job, repository, path, line and column it concerns, each null when it doesn't apply or is unknown. Problems that keep the file from being read or parsed SHALL be reported as errors in the same document. The format version SHALL change only for an incompatible change. Nothing SHALL be written to standard error unless the document itself can't be produced.

The document SHALL be described by a JSON Schema and examples kept in the repository, and the tests SHALL check real output against that schema.

#### Scenario: Valid with a warning
- **WHEN** the config is valid but has one warning
- **THEN** the document SHALL have `valid: true` and one problem with severity `warning`, and the exit status SHALL be 0

#### Scenario: YAML syntax error
- **WHEN** the file is not valid YAML
- **THEN** the document SHALL have `valid: false` and one error with the line the YAML parser reported, and the exit status SHALL be 1

#### Scenario: File missing
- **WHEN** the config file doesn't exist
- **THEN** the document SHALL have `valid: false` and one error saying the file couldn't be read, with no line

### Requirement: Validation Needs Nothing From the Host
`validate`, with or without `--json`, SHALL NOT read the host key, the state directory or the source paths, and SHALL NOT run restic or a hook. Locked values SHALL be checked for being well-formed only. The result SHALL therefore be the same for the same config file on any machine, with one exception: a job's `read_as: podman-unshare` is checked against the operating system validate runs on, and whether podman is installed there.

#### Scenario: Validating another host's config
- **WHEN** a config whose values are locked for another host is validated on a machine with no host key
- **THEN** the result SHALL be the same as on that host
