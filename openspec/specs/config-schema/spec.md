# config-schema Specification

## Purpose

Describes the config file's structure in a standard form, so that an editor can complete and check a config as it is typed.

## Requirements

### Requirement: Config JSON Schema
`rest-o-matic schema` SHALL print a JSON Schema (draft-07) describing the config file, built into the binary, so that each release describes its own config. The schema SHALL describe every key rest-o-matic reads, with a description of each, the allowed values where they are a fixed set (such as `backend`, `schedule`, retention periods and `read_as`), and both forms of the keys that accept two (a repository given by name or with retention overrides; `after` hooks as a list or by outcome). It SHALL reject keys rest-o-matic doesn't read, except top-level keys starting with `x-`. A locked or `!plain` value SHALL be described as a string.

The tests SHALL check that every key the config types read is in the schema and that the schema has no key they don't read, that the example config in the repository is valid against it, and that it rejects a set of configs `validate` rejects for their structure.

#### Scenario: Printed
- **WHEN** `rest-o-matic schema` is run
- **THEN** it SHALL print the schema to standard output and need no config file

#### Scenario: Example config
- **WHEN** `rest-o-matic.example.yaml` is checked against the schema
- **THEN** it SHALL be valid

#### Scenario: Unknown key
- **WHEN** a config has `hook:` instead of `hooks:` under a job
- **THEN** it SHALL NOT be valid against the schema
