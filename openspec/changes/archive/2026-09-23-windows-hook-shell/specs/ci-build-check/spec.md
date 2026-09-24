## ADDED Requirements

### Requirement: Windows Hook Check
Alongside the existing build and test suite, every pull request targeting `main` SHALL also build the project and run the hook runner's tests on Windows, under the same triggers as the existing check (opened, and re-run on each new commit). A failure there SHALL be reported as a failed check on the pull request.

#### Scenario: Windows hook tests run on a pull request
- **WHEN** a pull request targeting `main` is opened or receives a new commit
- **THEN** the project SHALL be built and the hook runner's tests SHALL run on a Windows runner against that commit

#### Scenario: A Windows-only failure is visible
- **WHEN** the hook tests pass on Linux but fail on Windows
- **THEN** the pull request SHALL show a failed Windows check
