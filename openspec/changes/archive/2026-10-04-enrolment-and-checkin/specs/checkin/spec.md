## Purpose

Keeps the central app up to date with what each host is doing, using one small request per tick, so that it can show every host's backups in one place and notice a host that has stopped reporting.

## ADDED Requirements

### Requirement: A Check-in at the Start of Every Tick
When the host is enrolled for the config file in use, every `tick` SHALL send one check-in to the central app after the config has loaded and the state directory check has passed, and before any job starts. `run` SHALL NOT check in. A host that is not enrolled, or is enrolled for a different config file, SHALL make no request.

The check-in SHALL be limited to 10 seconds when it carries no part content, and to 60 seconds when it does. Whatever its result, the tick SHALL then evaluate and run due jobs exactly as it would have without it, and its exit status SHALL not depend on the check-in.

#### Scenario: Every tick checks in
- **WHEN** an enrolled host's `tick` finds no job due
- **THEN** it SHALL still send a check-in

#### Scenario: Central app unreachable
- **WHEN** the central app can't be reached
- **THEN** the tick SHALL give up on the check-in within its time limit, run its due jobs, and exit as it otherwise would

#### Scenario: Different config file
- **WHEN** a host enrolled for `/home/me/backups/rest-o-matic.yaml` runs `tick` with `--config /tmp/test.yaml`
- **THEN** no request SHALL be made

### Requirement: Heartbeat With Parts Sent Only When Changed
Every check-in SHALL carry the host's ID, the time it was sent, the host's hostname, operating system, architecture and the versions of rest-o-matic and restic, and a fingerprint of each of three parts: *status*, *snapshots* and *config*.

The content of a part SHALL be included only when its fingerprint differs from the last one the central app acknowledged, or when the central app has asked for that part again. A part is acknowledged when a check-in that included it receives a successful reply. When the reply asks for a part again, it SHALL be included in the next check-in whatever its fingerprint.

- *status* SHALL be every job's state and recorded runs, as `status --json` reports them for each job, without the time the report was made or the time of the last tick.
- *snapshots* SHALL be every job's recorded snapshot lists.
- *config* SHALL be the config file's exact text.

#### Scenario: Nothing changed
- **WHEN** a check-in follows an acknowledged one and no job has run, started or become due, no snapshot list has changed and the config file is unchanged
- **THEN** it SHALL carry fingerprints only, and no part content

#### Scenario: A job finished
- **WHEN** a job finishes between two ticks
- **THEN** the next check-in SHALL include the *status* part, and the *snapshots* part if the job's lists changed, and SHALL NOT include the *config* part

#### Scenario: First check-in after enrolling
- **WHEN** a host checks in for the first time
- **THEN** every part SHALL be included

#### Scenario: Not acknowledged
- **WHEN** a check-in that included the *status* part fails
- **THEN** the next check-in SHALL include the *status* part again

#### Scenario: Central app asks again
- **WHEN** a reply lists `snapshots` as wanted again
- **THEN** the next check-in SHALL include the *snapshots* part even though it is unchanged

### Requirement: Config Withheld While It Holds Plain-Text Secrets
The *config* part SHALL NOT carry the config text while any repository's `password`, or any value under a repository's `env`, is written in plain text, with two exceptions: an `env` value whose name is on rest-o-matic's fixed list of settings known not to be secret (such as `AWS_DEFAULT_REGION` or `RESTIC_COMPRESSION`), and an `env` value marked `!plain`, MAY be plain text. Any other name SHALL be treated as a secret. While withheld, the part SHALL carry its fingerprint and the reason, naming the repositories and fields concerned. Locked values, `password_file` and `password_command` SHALL NOT prevent it being sent. Hook commands SHALL be sent as written.

#### Scenario: All secrets locked
- **WHEN** every repository's password and `env` values are locked or come from `password_file` or `password_command`
- **THEN** the config text SHALL be sent when it changes

#### Scenario: A harmless setting in plain text
- **WHEN** every secret is locked and repository `offsite` has `env: {AWS_DEFAULT_REGION: eu-west-1}` in plain text
- **THEN** the config text SHALL be sent

#### Scenario: A value marked as not secret
- **WHEN** every secret is locked and repository `offsite` has `env: {MY_BUCKET_PREFIX: !plain "host-a/"}`
- **THEN** the config text SHALL be sent

#### Scenario: An unknown env name in plain text
- **WHEN** repository `offsite` has `env: {MY_STORAGE_TOKEN: abc}` in plain text
- **THEN** the config text SHALL NOT be sent, and the part SHALL name `offsite`'s `MY_STORAGE_TOKEN`

#### Scenario: A plain-text password
- **WHEN** repository `nas` has `password: correct-horse`
- **THEN** the config text SHALL NOT be sent, and the part SHALL say it is withheld because of `nas`'s password

### Requirement: Failure Handling and Warnings
A check-in that fails SHALL never fail the tick. The host SHALL record the time of every attempt, the time of the last success, and the last error. It SHALL print a warning to standard error when a check-in fails after the previous one succeeded, and a notice when one succeeds after the previous one failed, and SHALL print nothing about check-ins otherwise.

A reply that rejects the host's credential SHALL be reported as such, saying the host must be enrolled again. A reply saying the message format is not supported SHALL be reported as such.

#### Scenario: Outage
- **WHEN** the central app is down for an hour and an enrolled host ticks every minute
- **THEN** exactly one warning SHALL be printed when the first check-in fails, and one notice when the first one succeeds again

#### Scenario: Host deleted in the central app
- **WHEN** the central app rejects the credential
- **THEN** the warning and `status` SHALL say that the host must be enrolled again

### Requirement: Checking In on Demand
`rest-o-matic checkin` SHALL send one check-in now, by the same rules as `tick`, and report the result and which parts were included. With `--print` it SHALL instead print the full check-in, with every part included, as JSON to standard output, and SHALL send nothing. `--print` SHALL work on a host that is not enrolled.

#### Scenario: Printed without enrolling
- **WHEN** `rest-o-matic checkin --print` is run on a host that has never enrolled
- **THEN** it SHALL print a check-in with every part and no host ID, and make no request

### Requirement: The Contract Is Written Down
The messages exchanged at enrolment and check-in SHALL be described in the repository, with a JSON Schema for each message and examples. The host's tests SHALL check the messages it actually sends against those schemas. Every message SHALL carry a format version, which SHALL change only for an incompatible change, and each side SHALL ignore fields it does not know.

#### Scenario: Real output matches the schema
- **WHEN** the test suite runs
- **THEN** a check-in produced by the host from a real config and state SHALL validate against the check-in schema
