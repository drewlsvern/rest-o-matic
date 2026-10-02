# state-directory Specification

## Purpose

Defines who may use the directory holding rest-o-matic's persisted state and lock files. The goal is that running rest-o-matic as the wrong user never leaves files behind that silently break later runs as the right user.

## Requirements

### Requirement: State Directory Owned by the Running User
On Linux and macOS, before a command creates or modifies anything in the state directory, rest-o-matic SHALL check that the state directory, and every lock or state file already in it, is owned by the user it is running as. If any of them has a different owner, the command SHALL fail before running any hook or restic command and before creating or modifying anything in the state directory. A state directory that does not exist yet SHALL pass the check. There SHALL be no option that skips it.

The check SHALL apply to `run` and `tick`. It SHALL apply to `exec` only when `exec` is about to take rest-o-matic's own per-repository lock. It SHALL NOT apply to `exec` invocations that do not take that lock (a shared-lock subcommand, or `--force`), or to `validate`.

#### Scenario: Root refused on a user's state directory
- **WHEN** rest-o-matic runs `tick` as root with a state directory owned by the user `me`
- **THEN** it SHALL fail without running any job, and SHALL NOT create any file in the state directory

#### Scenario: User refused when earlier root runs left files behind
- **WHEN** rest-o-matic runs `run documents` as `me`, and the state directory is owned by `me` but its `locks/repo-nas.lock` is owned by root
- **THEN** it SHALL fail without running the job

#### Scenario: Missing state directory passes
- **WHEN** rest-o-matic runs `run documents` and the state directory does not exist
- **THEN** the check SHALL pass and the job SHALL run, creating the state directory as the running user

#### Scenario: Matching owner passes
- **WHEN** rest-o-matic runs `tick` as root with a state directory and files all owned by root
- **THEN** the check SHALL pass

#### Scenario: Read-only exec is not checked
- **WHEN** rest-o-matic runs as root `exec nas -- restore <snapshot-id> --target /`, with a state directory owned by `me`
- **THEN** exec SHALL invoke restic, and SHALL NOT create any file in the state directory

#### Scenario: Locking exec is checked
- **WHEN** rest-o-matic runs as root `exec nas -- forget --tag documents --keep-daily 7`, without `--force`, with a state directory owned by `me`
- **THEN** exec SHALL refuse without invoking restic

#### Scenario: Windows is not checked
- **WHEN** rest-o-matic runs any command on Windows
- **THEN** no ownership check SHALL be performed

### Requirement: Ownership Error Explains the Fix
When the ownership check fails, the error SHALL name the user rest-o-matic is running as and the owner of each mismatched path, and list those paths. If the state directory itself is owned by another user, the error SHALL state both fixes: run as that user, or use a separate `--state-dir`. If the state directory is owned by the running user and only files inside it are not, the error SHALL instead say to remove the listed files or change their owner to the running user. For `exec`, the message SHALL use exec's own message prefix, and exec SHALL exit with a reserved exit code distinct from its existing reserved codes and from restic's documented exit codes.

#### Scenario: Error names users, paths and fixes
- **WHEN** rest-o-matic runs `tick` as root and the state directory `.rest-o-matic` is owned by `me`
- **THEN** the error SHALL name `root` as the running user, `me` as the owner and `.rest-o-matic` as the path, and SHALL suggest running as `me` or passing `--state-dir`

#### Scenario: Leftover files get a remove-or-chown fix
- **WHEN** rest-o-matic runs `tick` as `me`, the state directory is owned by `me`, and `locks/repo-nas.lock` is owned by root
- **THEN** the error SHALL list `locks/repo-nas.lock` as owned by `root` and SHALL say to remove it or change its owner to `me`

#### Scenario: Exec uses a reserved exit code
- **WHEN** exec refuses because of the ownership check
- **THEN** it SHALL exit with a reserved exit code different from those used for the lock guard and the tag-safety gate
