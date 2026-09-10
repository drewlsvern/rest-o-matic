## Purpose

Lets a user determine exactly which build of rest-o-matic they are running, sourced from the binary's own build information rather than a value someone has to remember to update by hand.

## ADDED Requirements

### Requirement: Version Derived from Build Information
The system SHALL derive its reported version from the running binary's build information rather than from a hardcoded value, so the reported version always reflects the actual source commit and tag state it was built from without requiring a manual update.

#### Scenario: Version differs across commits with no version-reporting code change
- **WHEN** binaries are built from two different commits
- **THEN** their reported version strings SHALL differ, without any change having been made to version-reporting code between the two builds

### Requirement: Short Version Line on No-Argument Invocation
Running the command with no arguments and no flags SHALL display a single-line version string, in addition to the command's existing usage/command listing output.

#### Scenario: No-args output includes the version line
- **WHEN** the command is invoked with no arguments
- **THEN** the output SHALL include a single line containing the version string, and SHALL still include the existing usage and available-commands listing

### Requirement: Detailed Build Information via --version
Invoking the command with `--version` SHALL display a multi-line block containing: the version string, the full source commit identifier, the commit timestamp, whether the working tree had uncommitted changes at build time, and the Go toolchain version used to build the binary.

#### Scenario: --version shows every required field
- **WHEN** `--version` is passed
- **THEN** the output SHALL include the version, the commit identifier, the commit time, the dirty-tree indicator, and the Go version, each identifiable as a distinct piece of information

### Requirement: No Separate Version Subcommand
The system SHALL NOT provide a subcommand dedicated to displaying version information. Version information SHALL be available only via the `--version` flag and the no-argument short line.

#### Scenario: No version subcommand exists
- **WHEN** a user looks for a subcommand dedicated to version information
- **THEN** none SHALL exist, and `--version` SHALL remain the only mechanism for the detailed build-information block

### Requirement: Existing Command Behavior Unaffected
Adding version reporting SHALL NOT change the behavior, flags, or output of any existing command.

#### Scenario: Existing commands are unaffected
- **WHEN** any existing command is invoked exactly as it was before version reporting was added
- **THEN** its behavior and output SHALL be unchanged, aside from the new short version line appearing only in the no-argument invocation case
