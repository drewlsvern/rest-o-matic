# ci-build-check Specification

## Purpose

Ensures a pull request cannot merge into `main` with a broken build or a failing test, by running the project's build and test suite automatically against every pull request targeting `main` and keeping that result current as the PR changes.

## Requirements

### Requirement: Build Check Runs on Pull Requests Targeting Main
The system SHALL run the project's build and test suite automatically whenever a pull request targeting `main` is opened, and SHALL re-run it whenever a new commit is pushed to that pull request's branch while it remains open.

#### Scenario: Check runs when a PR is opened
- **WHEN** a pull request targeting `main` is opened
- **THEN** the build and test suite SHALL run against that pull request's current head commit

#### Scenario: Check re-runs on new commits
- **WHEN** a new commit is pushed to an already-open pull request's branch
- **THEN** the build and test suite SHALL run again against the new head commit

#### Scenario: Nothing runs without an open pull request
- **WHEN** commits are pushed to a branch that has no open pull request targeting `main`
- **THEN** the build and test suite SHALL NOT run

### Requirement: Check Reflects the Actual Current Head
The result of the build and test suite SHALL always correspond to the pull request's actual current head commit, never to an earlier commit that has since been superseded by new pushes.

#### Scenario: A stale result is never presented as current
- **WHEN** a pull request's branch receives a new commit after a previous check result was recorded
- **THEN** the previous result SHALL NOT be presented as reflecting the new head; a fresh run against the new head SHALL supersede it

### Requirement: Merge Gate Depends on This Check
A pull request targeting `main` SHALL NOT be mergeable unless this check has passed against its current head commit, in addition to any required review approval.

#### Scenario: A failing or absent check blocks merging
- **WHEN** a pull request's current head has not passed the build and test suite (including the case where it has not yet been evaluated)
- **THEN** the pull request SHALL NOT be mergeable into `main`

### Requirement: Windows Hook Check
Alongside the existing build and test suite, every pull request targeting `main` SHALL also build the project and run the hook runner's tests on Windows, under the same triggers as the existing check (opened, and re-run on each new commit). A failure there SHALL be reported as a failed check on the pull request.

#### Scenario: Windows hook tests run on a pull request
- **WHEN** a pull request targeting `main` is opened or receives a new commit
- **THEN** the project SHALL be built and the hook runner's tests SHALL run on a Windows runner against that commit

#### Scenario: A Windows-only failure is visible
- **WHEN** the hook tests pass on Linux but fail on Windows
- **THEN** the pull request SHALL show a failed Windows check
