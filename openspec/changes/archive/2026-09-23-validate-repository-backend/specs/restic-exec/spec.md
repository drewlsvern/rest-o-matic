## MODIFIED Requirements

### Requirement: Repository-Anchored Invocation
The exec command SHALL take a configured repository's name as its target and SHALL inject that repository's connection URL and credentials into the restic invocation, the same way they are supplied for backup and forget. Referencing a repository name that is not defined SHALL be rejected before any restic process is started. The target repository's backend/url consistency check (as defined by the config capability) SHALL be applied before any restic process is started: a consistency error SHALL reject the invocation, and any repository warnings SHALL be printed to standard error before restic runs. Validation problems elsewhere in the config (other repositories, jobs, or policies) SHALL NOT block exec.

#### Scenario: Exec targets a configured repository
- **WHEN** a user runs exec against a repository defined in the config, followed by a restic subcommand and its arguments
- **THEN** the repository's URL and credentials SHALL be supplied to that restic invocation without the user specifying them

#### Scenario: Undefined repository is rejected
- **WHEN** a user runs exec against a repository name not present in the config
- **THEN** the command SHALL fail before attempting to invoke restic

#### Scenario: Repository with backend/url contradiction is rejected
- **WHEN** a user runs exec against a repository declaring `backend: s3` whose url lacks the `s3:` prefix
- **THEN** the command SHALL fail with a message identifying the problem, before attempting to invoke restic

#### Scenario: Repository warnings printed but exec proceeds
- **WHEN** a user runs exec against a repository that produces only warnings (e.g. `backend: local` with a relative path)
- **THEN** the warnings SHALL be printed to standard error and restic SHALL then be invoked normally

#### Scenario: Unrelated config problems do not block exec
- **WHEN** a user runs exec against a valid repository while a job elsewhere in the config references an undefined policy
- **THEN** exec SHALL invoke restic against the target repository
