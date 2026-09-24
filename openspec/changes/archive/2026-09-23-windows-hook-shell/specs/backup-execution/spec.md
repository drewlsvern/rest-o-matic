## ADDED Requirements

### Requirement: Platform Hook Shell
Each hook command SHALL be run as a single command string by the platform's native shell: `sh -c` on Linux and macOS, and `cmd.exe` on Windows. On Windows the command string SHALL reach `cmd.exe` exactly as written in the config, including any double quotes, without extra escaping added by rest-o-matic. Hook commands therefore use the syntax of that platform's shell (for example `%RESTOMATIC_JOB%` on Windows, `$RESTOMATIC_JOB` on Linux and macOS).

#### Scenario: Hook runs on Windows without sh installed
- **WHEN** a hook `echo hello` runs on Windows on a machine with no `sh` on `PATH`
- **THEN** the hook SHALL run successfully through `cmd.exe`

#### Scenario: Windows hook uses cmd variable syntax
- **WHEN** a hook `if "%RESTOMATIC_JOB%"=="documents" (exit 0) else (exit 1)` runs on Windows for the job `documents`
- **THEN** the hook SHALL exit successfully

#### Scenario: Quoted arguments reach the command unchanged on Windows
- **WHEN** a Windows hook passes a double-quoted argument containing spaces, e.g. `echo "a  b"`
- **THEN** the program SHALL receive the argument exactly as written, with no backslash-escaped quotes added

#### Scenario: Linux and macOS unchanged
- **WHEN** a hook runs on Linux or macOS
- **THEN** it SHALL run through `sh -c` exactly as before

### Requirement: Stopping a Hook Stops What It Started
When rest-o-matic stops a running hook (because of an interrupt, or because the cleanup time limit expired), it SHALL stop the hook's shell and every process started by it, on every supported platform. On Linux and macOS the processes SHALL first be asked to exit (SIGTERM) and killed only if they are still running shortly after. On Windows they SHALL be terminated immediately.

#### Scenario: Child process stopped on Windows
- **WHEN** a Windows hook starts a long-running program and the hook is stopped
- **THEN** both `cmd.exe` and the program it started SHALL no longer be running

#### Scenario: Child process stopped on Linux
- **WHEN** a Linux hook starts a long-running background program and the hook is stopped
- **THEN** both the shell and the program it started SHALL no longer be running
