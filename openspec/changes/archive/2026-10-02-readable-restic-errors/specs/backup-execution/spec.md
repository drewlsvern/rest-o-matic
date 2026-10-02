## ADDED Requirements

### Requirement: Restic Failures Are Reported as Readable Messages
When a restic command run for a job fails, the error rest-o-matic reports SHALL contain restic's exit status and restic's own messages as plain text. Where restic reports an error as a JSON object, the reported error SHALL contain the message from that object and SHALL NOT contain the JSON itself. Lines restic prints as plain text SHALL be kept. A leading `Fatal: ` SHALL be dropped, and only the first line of a multi-line message SHALL be kept.

When restic reports more than a few separate errors, the first few SHALL be reported followed by a count of the rest.

This SHALL apply wherever the error surfaces: the output of `run` and `tick`, the recorded run, `status`, and the `RESTOMATIC_ERROR` given to hooks and notifications. It SHALL NOT change the output of `exec`, which remains restic's own.

#### Scenario: Repository does not exist
- **WHEN** a job backs up to a repository that was never initialised
- **THEN** the reported error SHALL say that the repository does not exist, SHALL include restic's exit status, and SHALL NOT contain `message_type` or any other JSON

#### Scenario: Wrong password
- **WHEN** a job backs up to a repository with the wrong password
- **THEN** the reported error SHALL say `wrong password or no key found`

#### Scenario: Unreadable source file
- **WHEN** a backup can't read one source file because permission is denied
- **THEN** the reported error SHALL name that file and say permission was denied, in plain text, and the hint about unreadable source files SHALL still be given

#### Scenario: Many errors are summarised
- **WHEN** a backup can't read twenty source files
- **THEN** the reported error SHALL list the first few and say how many more there were

#### Scenario: Notification text is readable
- **WHEN** a job fails because its repository does not exist and a `failure` notification command uses `RESTOMATIC_ERROR`
- **THEN** `RESTOMATIC_ERROR` SHALL be a plain sentence containing `repository does not exist`
