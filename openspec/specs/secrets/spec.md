# secrets Specification

## Purpose

Lets secrets in the config be stored encrypted, so that the config file can be copied, shared and stored elsewhere without exposing repository passwords or storage credentials, while the host that needs them can still use them.

## Requirements

### Requirement: Host Key
Each user that runs rest-o-matic on a host MAY have a host key: a private key from which a public key can be derived. `secret keygen` SHALL create it, in a per-user location outside the config and the state directory, readable only by its owner. If a host key already exists, `secret keygen` SHALL refuse and SHALL leave the existing key untouched. The location SHALL be overridable with `--key-file`.

On Linux and macOS, a host key file that is readable by anyone other than its owner SHALL NOT be used: any command that needs it SHALL fail with an error naming the file and its permissions.

`secret public-key` SHALL print the host's public key. It SHALL fail if there is no host key.

#### Scenario: Key created
- **WHEN** `secret keygen` is run and no host key exists
- **THEN** a host key SHALL be created readable only by its owner, and its public key and location SHALL be printed

#### Scenario: Existing key is never overwritten
- **WHEN** `secret keygen` is run and a host key already exists
- **THEN** the command SHALL fail and the existing key SHALL be unchanged

#### Scenario: Key readable by others is refused
- **WHEN** the host key file has mode `0644` and `run` needs to unlock a value
- **THEN** it SHALL fail with an error naming the key file and saying its permissions are too open

#### Scenario: Public key printed
- **WHEN** `secret public-key` is run on a host with a key
- **THEN** it SHALL print that key's public key and nothing else on standard output

### Requirement: Locking a Value
`secret lock` SHALL read a secret value and print its locked form, ready to be placed in the config. It SHALL read the value from standard input when that is not a terminal, and otherwise SHALL prompt for it without echoing it. It SHALL refuse an empty value. The plain text SHALL NOT appear in its output.

The locked value SHALL be openable by each of: this host's key, if one exists; every recovery key listed in the recovery recipients file kept beside the host key; and every public key given with `--recipient`. If that leaves no key at all, the command SHALL fail and say how to create a host key.

Locking the same value twice SHALL produce different output each time.

#### Scenario: Value locked for the host
- **WHEN** `secret lock` is given `correct-horse` on standard input on a host with a key
- **THEN** it SHALL print a locked form that this host can unlock back to `correct-horse`, and that does not contain `correct-horse`

#### Scenario: Recovery key can also open it
- **WHEN** the recovery recipients file lists a recovery public key and `secret lock` is run
- **THEN** the locked value SHALL be openable with the host key and, separately, with the recovery private key

#### Scenario: Locking for another host
- **WHEN** `secret lock --recipient <another host's public key>` is run
- **THEN** the locked value SHALL be openable with that other host's key

#### Scenario: No key to lock for
- **WHEN** `secret lock` is run with no host key, no recovery recipients and no `--recipient`
- **THEN** it SHALL fail and mention `secret keygen`

#### Scenario: Empty value refused
- **WHEN** `secret lock` is given an empty value
- **THEN** it SHALL fail without printing a locked form

### Requirement: Locked Values Are Unlocked Only in Memory
When a command starts restic for a repository, it SHALL unlock that repository's locked `password` and locked `env` values using the host key and pass the plain text to restic the same way a plain-text value is passed. The plain text SHALL NOT be written to any file, and SHALL NOT appear in rest-o-matic's own output or in the state file. Only the repositories a command actually uses SHALL be unlocked.

This SHALL apply equally to backups, retention enforcement and `exec`.

#### Scenario: Backup with a locked password
- **WHEN** a job backs up to a repository whose `password` is locked for this host
- **THEN** the backup SHALL succeed exactly as it would with the same password in plain text

#### Scenario: Locked env value reaches restic
- **WHEN** a repository has `env: {AWS_SECRET_ACCESS_KEY: !locked "..."}` locked for this host
- **THEN** restic SHALL be started with `AWS_SECRET_ACCESS_KEY` set to the plain text

#### Scenario: Exec with a locked password
- **WHEN** `exec nas -- snapshots` is run and `nas` has a locked password
- **THEN** restic SHALL be invoked with the unlocked password

#### Scenario: Unused repository is not unlocked
- **WHEN** `exec nas -- snapshots` is run, and a different repository `offsite` has a value locked for some other host
- **THEN** the command SHALL succeed

### Requirement: A Value That Cannot Be Unlocked Fails That Repository
If a repository's locked value cannot be unlocked, because there is no host key or the value was not locked for this host, the operation against that repository SHALL fail without starting restic. The error SHALL name the repository and the field, and SHALL say which of the two reasons applies. It SHALL NOT contain any part of the locked value.

Within a job this SHALL be treated like any other failure of that repository: the job's other repositories SHALL still be attempted and the job SHALL be reported as failed.

#### Scenario: No host key
- **WHEN** a job backs up to a repository with a locked password and the host has no key
- **THEN** that repository's backup SHALL fail with an error naming the repository, saying its password is locked and no host key was found, and restic SHALL NOT be started for it

#### Scenario: Locked for a different host
- **WHEN** a repository's password was locked only for another host's key
- **THEN** the backup SHALL fail with an error saying the value was not locked for this host's key

#### Scenario: Other repositories still run
- **WHEN** a job backs up to `nas`, whose password is plain, and `offsite`, whose locked password cannot be unlocked
- **THEN** the backup to `nas` SHALL be attempted, and the job SHALL be reported as failed because of `offsite`

### Requirement: Revealing a Locked Value
`secret reveal <repository>` SHALL print the plain text of that repository's locked `password`, and `secret reveal <repository> <NAME>` SHALL print the plain text of its locked `env` value `NAME`, using the host key. It SHALL print the value and nothing else on standard output. It SHALL fail, without printing a value, when the repository or the `env` name does not exist, when the value is not locked, or when it cannot be unlocked.

#### Scenario: Password revealed
- **WHEN** `secret reveal offsite` is run and `offsite` has a password locked for this host
- **THEN** the plain-text password SHALL be printed

#### Scenario: Env value revealed
- **WHEN** `secret reveal offsite AWS_SECRET_ACCESS_KEY` is run and that value is locked for this host
- **THEN** its plain text SHALL be printed

#### Scenario: Value is not locked
- **WHEN** `secret reveal nas` is run and `nas` has a plain-text password
- **THEN** the command SHALL fail and say the value is not locked

### Requirement: Checking That Every Locked Value Can Be Unlocked
`secret check` SHALL attempt to unlock every locked value in the config with the host key and report, for each one that fails, the repository, the field and the reason. It SHALL exit with status zero only if every locked value can be unlocked. It SHALL NOT print any plain text. A config with no locked values SHALL pass.

#### Scenario: Everything unlocks
- **WHEN** `secret check` is run and every locked value in the config was locked for this host
- **THEN** it SHALL report success and exit with status zero

#### Scenario: One value locked for another host
- **WHEN** `secret check` is run and `offsite`'s password was locked only for another host
- **THEN** it SHALL name `offsite` and its password, and exit non-zero

### Requirement: Locked Values Are Standard age Files
The locked form SHALL be an [age](https://age-encryption.org)-encrypted file, base64-encoded onto a single line. The host key file SHALL be a standard age identity file, and the recovery recipients file a standard age recipients file. A locked value SHALL therefore be openable with the stock `age` tool and a matching key, without rest-o-matic.

#### Scenario: Opened with the age tool
- **WHEN** a locked value is base64-decoded and passed to `age --decrypt` with the host key file as the identity
- **THEN** `age` SHALL output the plain text
