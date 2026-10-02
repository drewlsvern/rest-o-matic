# restic-exec Specification

## Purpose

Lets a user run any restic subcommand against a configured repository, with rest-o-matic supplying the repository's connection info, while preserving the safety guarantees the rest of rest-o-matic depends on when a single repository is shared by more than one job.

## Requirements

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

### Requirement: Transparent Output and Exit Code Passthrough
Exec SHALL connect standard input, standard output, and standard error directly between the user and the restic subprocess, without capturing, buffering, or reinterpreting restic's output. When restic is actually invoked, exec SHALL exit with restic's own exit code, unmodified.

#### Scenario: Restic's output reaches the user unmodified
- **WHEN** a restic subcommand invoked through exec writes to standard output or standard error
- **THEN** that output SHALL reach the user exactly as restic produced it, with nothing added, removed, or reformatted

#### Scenario: Restic's exit code is propagated
- **WHEN** a restic subcommand invoked through exec exits with a given status code
- **THEN** exec SHALL exit with that same status code

### Requirement: Lock-Type-Based Concurrency Guard
Before invoking restic, exec SHALL determine whether the requested subcommand is known to require only a shared (non-exclusive) lock at the restic level. If so, exec SHALL proceed without acquiring rest-o-matic's own per-repository execution guard. For every other subcommand - including any subcommand not recognized - exec SHALL require the same per-repository execution guard used by backup and forget before proceeding, deferring the command entirely if that guard is currently held by another execution.

#### Scenario: A known shared-lock subcommand runs without waiting on rest-o-matic's guard
- **WHEN** a user runs exec with a subcommand known to require only a shared lock, and another execution currently holds the per-repository guard for that same repository
- **THEN** exec SHALL still proceed to invoke restic for this subcommand

#### Scenario: An unrecognized subcommand defaults to requiring the guard
- **WHEN** a user runs exec with a subcommand not recognized as shared-lock-only
- **THEN** exec SHALL require the per-repository execution guard before invoking restic

#### Scenario: A guarded subcommand is deferred when the guard is held
- **WHEN** a user runs exec with a subcommand that requires the per-repository execution guard, and another execution currently holds it for that repository
- **THEN** exec SHALL NOT invoke restic, and SHALL report that the repository is currently in use

### Requirement: Force Override Scoped to the Concurrency Guard Only
A force option SHALL cause exec to skip the per-repository execution guard entirely for that invocation, regardless of how the subcommand would otherwise be classified. The force option SHALL have no effect on the tag-safety gate described below.

#### Scenario: Force skips the concurrency guard
- **WHEN** a user runs exec with the force option against a repository whose execution guard is currently held by another execution
- **THEN** exec SHALL proceed to invoke restic without waiting for or acquiring that guard

#### Scenario: Force does not bypass the tag-safety gate
- **WHEN** a user runs exec with the force option, targeting a subcommand and repository that would otherwise be refused by the tag-safety gate
- **THEN** exec SHALL still refuse to invoke restic for that reason

### Requirement: Tag-Safety Gate for Forget and Tag
When the requested subcommand is forget or tag, and the target repository is referenced by more than one configured job, exec SHALL refuse to invoke restic unless the user's own arguments already include a tag filter. When the repository is referenced by only one job, this gate SHALL NOT apply.

#### Scenario: Untagged forget against a shared repository is refused
- **WHEN** a user runs exec targeting forget against a repository referenced by more than one job, without including a tag filter in their arguments
- **THEN** exec SHALL refuse to invoke restic

#### Scenario: Tagged forget against a shared repository is allowed
- **WHEN** a user runs exec targeting forget against a repository referenced by more than one job, including a tag filter in their arguments
- **THEN** exec SHALL proceed to invoke restic

#### Scenario: Forget against a single-job repository is not gated
- **WHEN** a user runs exec targeting forget against a repository referenced by only one job, without including a tag filter
- **THEN** exec SHALL proceed to invoke restic

### Requirement: Tag-Safety Gate for Restore with Relative Selectors
When the requested subcommand is restore, the target repository is referenced by more than one configured job, and the snapshot being restored is specified with a relative selector rather than an explicit snapshot ID, exec SHALL refuse to invoke restic unless the user's own arguments already include a tag filter. Restoring an explicit snapshot ID SHALL NOT be gated, regardless of how many jobs reference the repository.

#### Scenario: Restoring "latest" without a tag against a shared repository is refused
- **WHEN** a user runs exec targeting restore of a relative selector against a repository referenced by more than one job, without including a tag filter
- **THEN** exec SHALL refuse to invoke restic

#### Scenario: Restoring an explicit snapshot ID is never gated
- **WHEN** a user runs exec targeting restore of an explicit snapshot ID against a repository referenced by more than one job, without including a tag filter
- **THEN** exec SHALL proceed to invoke restic

### Requirement: Prune Exempt from the Tag-Safety Gate
The tag-safety gate SHALL NOT apply to the prune subcommand, regardless of how many jobs reference the target repository.

#### Scenario: Prune against a shared repository is not gated
- **WHEN** a user runs exec targeting prune against a repository referenced by more than one job
- **THEN** exec SHALL proceed to invoke restic without requiring a tag filter

### Requirement: No Bypass Flag for the Tag-Safety Gate
There SHALL be no command-line option that suppresses or overrides the tag-safety gate. The only ways to proceed with a gated command SHALL be supplying a tag filter in the command's own arguments, or invoking restic outside of exec.

#### Scenario: No option overrides the gate
- **WHEN** a user runs exec with any combination of documented options, targeting a command and repository the tag-safety gate would refuse
- **THEN** exec SHALL still refuse to invoke restic unless a tag filter is present in the user's own arguments

### Requirement: Distinguishable Messages from Rest-o-matic Itself
Every message that exec produces itself - as opposed to output passed through from restic - SHALL be consistently distinguishable from restic's own output, so a user can tell which of the two produced a given line.

#### Scenario: A rest-o-matic message is visually distinct from restic output
- **WHEN** exec refuses to invoke restic for any reason and prints a message explaining why
- **THEN** that message SHALL be presented in a way that is consistently distinguishable from restic's own output

### Requirement: Blocked-by-Lock Message Content
When exec defers a command because the per-repository execution guard is held by another execution, the resulting message SHALL name the repository, state that it is in use by another execution, and inform the user that they may retry later or use the force option.

#### Scenario: Lock-blocked message names the repository and mentions the force option
- **WHEN** exec refuses to invoke restic because the per-repository execution guard is held elsewhere
- **THEN** the message SHALL name the repository and SHALL mention that the force option is available to proceed anyway

### Requirement: Blocked-by-Gate Message Content
When exec refuses a command because of the tag-safety gate, the resulting message SHALL name every job that references the target repository, and SHALL state that no option exists to bypass this particular check.

#### Scenario: Gate-blocked message names the sharing jobs
- **WHEN** exec refuses to invoke restic because of the tag-safety gate
- **THEN** the message SHALL list the names of every job that references the target repository, and SHALL state that this check cannot be bypassed with an option

### Requirement: Reserved Exit Codes for Rest-o-matic-Refused Executions
When exec refuses to invoke restic at all, it SHALL exit with one of a small set of exit codes reserved for that purpose, distinct from each other for the lock-guard and tag-safety-gate cases, and none of which collide with an exit code restic itself documents. These reserved codes SHALL only be used when restic was never invoked; whenever restic is actually invoked, its own exit code is propagated instead, per the transparent passthrough requirement above.

#### Scenario: Lock-blocked and gate-blocked refusals use distinct exit codes
- **WHEN** exec refuses a command once because of the concurrency guard and once because of the tag-safety gate
- **THEN** the two refusals SHALL produce different, reserved exit codes, neither of which is used by restic's own documented exit codes

#### Scenario: A reserved exit code is never produced alongside an actual restic invocation
- **WHEN** restic is actually invoked by exec, regardless of whether it succeeds or fails
- **THEN** the exit code SHALL be restic's own, never one of the reserved rest-o-matic-only codes

### Requirement: Job-Scoped Exec
Exec SHALL accept an optional `--job <name>` option. When it is given, the named job SHALL exist and SHALL back up to the target repository. Otherwise exec SHALL refuse before invoking restic, with a message naming the job and the repository. When the job is valid, restic SHALL be invoked in that job's read mode, whatever the subcommand, so that files restored from a `podman-unshare` job's snapshots get back their original owners. Every other exec behaviour (credentials, output and exit-code passthrough, the concurrency guard, the tag-safety gate) SHALL be unchanged. `--job` SHALL NOT add a tag filter or otherwise change the user's restic arguments.

Without `--job`, exec SHALL invoke restic directly, as before.

#### Scenario: Restore through a podman-unshare job
- **WHEN** a user runs `exec nas --job gitea -- restore <snapshot-id> --target /`, where `gitea` has `read_as: podman-unshare` and backs up to `nas`
- **THEN** restic SHALL run inside the Podman user namespace, and restored files SHALL have the host ownership they had when backed up

#### Scenario: Job not using the repository is refused
- **WHEN** a user runs `exec offsite --job gitea -- snapshots`, and `gitea` does not back up to `offsite`
- **THEN** exec SHALL refuse without invoking restic, naming `gitea` and `offsite`

#### Scenario: Unknown job is refused
- **WHEN** a user runs `exec nas --job nope -- snapshots` and no job `nope` exists
- **THEN** exec SHALL refuse without invoking restic

#### Scenario: Direct job changes nothing
- **WHEN** a user runs `exec nas --job documents -- snapshots`, where `documents` uses read mode `direct`
- **THEN** restic SHALL be invoked exactly as without `--job`

#### Scenario: --job does not satisfy the tag-safety gate
- **WHEN** a user runs `exec nas --job gitea -- forget --keep-daily 7` against a repository shared by several jobs, without `--tag`
- **THEN** exec SHALL still refuse because of the tag-safety gate

### Requirement: Restore Must Match the Job's Read Mode
When exec is given `--job` for a job whose read mode is not `direct`, and the subcommand is `restore`, exec SHALL confirm, before invoking restic, that the snapshot being restored was taken in that read mode, that is, that it carries the tag `restomatic-read=<mode>`. Restoring a snapshot in a different mode from the one it was taken in would give restored files, and the existing parent directories restic restores metadata for, the wrong owners.

- For an explicit snapshot ID (optionally followed by `:<subfolder>`), exec SHALL look up that snapshot's tags and SHALL refuse if the snapshot does not carry the read-mode tag, or if the lookup fails.
- For `latest`, exec SHALL refuse unless every `--tag` filter in the user's arguments includes the read-mode tag, so restic can only select a matching snapshot.
- If exec cannot tell which snapshot is being restored, it SHALL refuse.

A refusal SHALL name the snapshot and the job's read mode, say how to restore instead (without `--job` for a snapshot taken in `direct` mode, or with `--tag restomatic-read=<mode>` for `latest`), and exit with a reserved exit code distinct from exec's other reserved codes and from restic's documented exit codes. `direct` jobs, `exec` without `--job`, and subcommands other than `restore` SHALL NOT be checked.

#### Scenario: Matching snapshot restores
- **WHEN** a user runs `exec nas --job gitea -- restore 9e0e530a --target /home/me/restore`, where `gitea` uses `podman-unshare` and snapshot `9e0e530a` carries `restomatic-read=podman-unshare`
- **THEN** exec SHALL invoke restic inside the Podman user namespace

#### Scenario: Snapshot taken in direct mode is refused
- **WHEN** a user runs `exec nas --job gitea -- restore 1a2b3c4d --target /home/me/restore`, where `gitea` uses `podman-unshare` and snapshot `1a2b3c4d` has no read-mode tag
- **THEN** exec SHALL refuse without invoking the restore, and SHALL say to restore that snapshot without `--job`

#### Scenario: latest without the read-mode tag is refused
- **WHEN** a user runs `exec nas --job gitea -- restore latest --tag gitea --target /home/me/restore`
- **THEN** exec SHALL refuse without invoking restic, and SHALL suggest `--tag gitea,restomatic-read=podman-unshare`

#### Scenario: latest with the read-mode tag restores
- **WHEN** a user runs `exec nas --job gitea -- restore latest --tag gitea,restomatic-read=podman-unshare --target /home/me/restore`
- **THEN** exec SHALL invoke restic inside the Podman user namespace

#### Scenario: Direct jobs and other subcommands are not checked
- **WHEN** a user runs `exec nas --job documents -- restore 1a2b3c4d --target /tmp/r`, where `documents` uses `direct`, or `exec nas --job gitea -- ls 1a2b3c4d`
- **THEN** exec SHALL invoke restic without checking the snapshot's read mode
