## Purpose

Turns a manually-created, valid version tag into a published GitHub Release carrying checksummed, multi-platform binaries and a changelog, while guarding against the tag being malformed or pointing at a commit that never went through the project's own build check.

## ADDED Requirements

### Requirement: Release Triggered Only by a Version Tag Push
The system SHALL run the release process when, and only when, a tag matching the project's version convention (`vMAJOR.MINOR.PATCH`, optionally followed by a hyphenated pre-release identifier) is pushed. Pushes to branches, and tags that do not match this convention, SHALL NOT trigger a release.

#### Scenario: A valid version tag triggers a release
- **WHEN** a tag such as `v1.2.3` or `v1.2.3-rc.1` is pushed
- **THEN** the release process SHALL run for that tag

#### Scenario: A branch push does not trigger a release
- **WHEN** commits are pushed to any branch without an accompanying version tag
- **THEN** the release process SHALL NOT run

### Requirement: No Automated Tag Creation
The system SHALL NOT create or push version tags on its own behalf, based on commit history or any other signal. Creating a version tag SHALL remain a deliberate, manually-initiated action.

#### Scenario: Merging to main does not create a release
- **WHEN** a pull request merges into `main`
- **THEN** no version tag SHALL be created, and no release SHALL be produced, as a result of that merge alone

### Requirement: Tag Format Validated Before Any Release Work
Before performing any other release work, the system SHALL validate that the triggering tag strictly matches the version convention. If it does not, the system SHALL abort with a clear error identifying the problem, without producing partial release artifacts.

#### Scenario: A malformed tag is rejected before any build work
- **WHEN** a pushed tag does not strictly match `vMAJOR.MINOR.PATCH` with an optional hyphenated pre-release identifier (for example, missing the `v` prefix, or missing a patch number)
- **THEN** the release process SHALL abort immediately with an error identifying the tag as invalid, and SHALL NOT proceed to build or publish anything

### Requirement: Tagged Commit Must Be an Ancestor of Main
Before performing any other release work, the system SHALL verify that the commit the triggering tag points to is reachable from `main`'s history. If it is not, the system SHALL abort the release without publishing anything.

#### Scenario: A tag on an unrelated commit is rejected
- **WHEN** a pushed version tag points to a commit that is not part of `main`'s history
- **THEN** the release process SHALL abort before producing or publishing any release artifacts

#### Scenario: A tag on a commit that is part of main's history proceeds
- **WHEN** a pushed version tag points to a commit that is reachable from `main`
- **THEN** the release process SHALL proceed

### Requirement: Multi-Platform Release Binaries
For a release that passes both prerequisite checks, the system SHALL produce binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`, each packaged in an archive appropriate to its platform.

#### Scenario: All five platform archives are produced
- **WHEN** a release runs to completion
- **THEN** an archive SHALL exist for each of `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`

### Requirement: Checksums Published Alongside Binaries
The system SHALL publish a checksum covering every release archive, alongside the archives themselves.

#### Scenario: A checksum file accompanies the release
- **WHEN** a release runs to completion
- **THEN** a checksums file covering every published archive SHALL be attached to the release

### Requirement: Changelog Grouped by Conventional Commit Type
The system SHALL generate a changelog for the release from the commits between the previous tag and this one, grouped by conventional-commit type (such as features versus fixes).

#### Scenario: The changelog groups commits by type
- **WHEN** a release runs to completion and the commit range since the previous tag contains commits using conventional-commit prefixes
- **THEN** the published release's changelog SHALL group those commits by their type rather than listing them as an undifferentiated list

### Requirement: Version-Correct Builds
Every published release binary SHALL, when run, report the exact tag that triggered the release as its version - not a pseudo-version, and not a placeholder such as `(devel)`.

#### Scenario: A release binary reports the exact tag
- **WHEN** a binary built and published for tag `v1.2.3` is run
- **THEN** it SHALL report its version as exactly `v1.2.3`

### Requirement: Pre-Release Tags Are Published as Pre-Releases
A tag carrying a hyphenated pre-release identifier SHALL be published as a pre-release, and SHALL NOT be presented as the latest stable release.

#### Scenario: A release-candidate tag is marked as a pre-release
- **WHEN** a tag such as `v1.2.3-rc.1` triggers a release
- **THEN** the resulting published release SHALL be marked as a pre-release, not as the latest release
