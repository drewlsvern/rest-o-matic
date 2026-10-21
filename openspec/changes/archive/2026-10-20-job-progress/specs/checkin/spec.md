## MODIFIED Requirements

### Requirement: Heartbeat With Parts Sent Only When Changed
Every check-in SHALL carry the host's ID, the time it was sent, the host's hostname, operating system, architecture and the versions of rest-o-matic and restic, and a fingerprint of each of three parts: *status*, *snapshots* and *config*.

The content of a part SHALL be included only when its fingerprint differs from the last one the central app acknowledged, or when the central app has asked for that part again. A part is acknowledged when a check-in that included it receives a successful reply. When the reply asks for a part again, it SHALL be included in the next check-in whatever its fingerprint.

- *status* SHALL be every job's state and recorded runs, as `status --json` reports them for each job, without the time the report was made, the time of the last tick, or a running job's progress.
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

#### Scenario: Progress does not change the status part
- **WHEN** a job is running across two ticks and its progress has moved between them, and nothing else has changed
- **THEN** the second check-in SHALL carry fingerprints only, and the *status* part SHALL contain no progress
