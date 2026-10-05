## Purpose

Lets a host be registered with the central app once, so that it can report to it from then on, without the central app ever connecting to the host.

## ADDED Requirements

### Requirement: Enrolling a Host
`rest-o-matic enrol <url>` SHALL register the host with the central app at `<url>` using a one-time enrolment token, given with `--token` or, when it is not given, read from a prompt that does not echo it. It SHALL send the token, the host's public key, its hostname, operating system and architecture, and the versions of rest-o-matic and restic. It SHALL create the host key first if there isn't one.

On success it SHALL store, readable only by its owner and beside the host key, the central app's address, the host ID and the credential the central app returned, and the absolute path of the config file it was enrolled for. It SHALL print the name the central app gave the host. `enroll` SHALL be accepted as another name for the command.

#### Scenario: Successful enrolment
- **WHEN** `rest-o-matic enrol https://backups.example.com --token <token>` is run with a valid token on a host with no host key
- **THEN** a host key SHALL be created, the enrolment SHALL be stored readable only by its owner, and the host's name in the central app SHALL be printed

#### Scenario: Token from a prompt
- **WHEN** `enrol` is run without `--token` in a terminal
- **THEN** it SHALL prompt for the token without echoing it

#### Scenario: Token refused
- **WHEN** the central app rejects the token as unknown, used or expired
- **THEN** enrolment SHALL fail with a message saying so, and nothing SHALL be stored

#### Scenario: Already enrolled
- **WHEN** `enrol` is run on a host that is already enrolled, without `--force`
- **THEN** it SHALL fail without contacting the central app and leave the existing enrolment in place

### Requirement: HTTPS Unless Explicitly Allowed
Enrolment and every later request SHALL use HTTPS and SHALL verify the central app's certificate. An `http://` address SHALL be refused unless `--allow-http` is given at enrolment, which SHALL be recorded and apply to later requests. `--ca-file` SHALL add a certificate authority to those trusted for this central app, and SHALL be recorded too. There SHALL be no option to skip certificate verification.

#### Scenario: Plain HTTP refused
- **WHEN** `enrol http://backups.example.com --token <token>` is run without `--allow-http`
- **THEN** it SHALL fail without sending anything

#### Scenario: Private certificate authority
- **WHEN** the central app's certificate is issued by a private authority and `--ca-file ca.pem` names that authority
- **THEN** enrolment SHALL succeed, and later check-ins SHALL trust the same authority

#### Scenario: Untrusted certificate
- **WHEN** the central app's certificate can't be verified
- **THEN** enrolment SHALL fail with an error about the certificate

### Requirement: Recovery Keys Need Confirmation
If the central app's reply offers recovery public keys, `enrol` SHALL show each one not already in the recovery recipients file and SHALL add it only when the user confirms it at a prompt, or names it with `--accept-recovery-key`. A key that is not confirmed SHALL NOT be added, and enrolment SHALL still complete, with a warning that values locked on this host can't be opened with that key.

#### Scenario: Key confirmed
- **WHEN** the reply offers a recovery key and the user confirms it
- **THEN** it SHALL be added to the recovery recipients file beside the host key

#### Scenario: Values locked before the key existed
- **WHEN** a recovery key is added during enrolment and the config already holds locked values
- **THEN** `enrol` SHALL say that those values can't be opened with the new key and, in a terminal, offer to re-lock them for this host's key and the recovery keys; if accepted it SHALL re-lock them as `secret relock` does, and otherwise, or without a terminal, it SHALL print the command that does it

#### Scenario: Not confirmed when nobody can answer
- **WHEN** `enrol` runs without a terminal and the reply offers a recovery key not named with `--accept-recovery-key`
- **THEN** the key SHALL NOT be added, and enrolment SHALL complete with a warning

### Requirement: Removing an Enrolment
`rest-o-matic unenrol` SHALL remove the stored enrolment, after which the host SHALL make no request to the central app. It SHALL NOT remove the host key or the recovery recipients file.

#### Scenario: Unenrolled host goes quiet
- **WHEN** `unenrol` is run and `tick` runs afterwards
- **THEN** the tick SHALL make no request to the central app
