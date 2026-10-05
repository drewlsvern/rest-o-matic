## ADDED Requirements

### Requirement: Check-in State Is Shown
For an enrolled host, `status` SHALL show when the host last checked in successfully, and, if the most recent attempt failed, when and why. It SHALL show when the config is being withheld from the central app and why. For a host that is not enrolled it SHALL say so. The JSON output SHALL carry the same information under `checkin`, with nulls where something has not happened.

#### Scenario: Failing check-ins
- **WHEN** the last successful check-in was three hours ago and every attempt since has failed with "connection refused"
- **THEN** `status` SHALL show that it last checked in three hours ago and is failing with "connection refused"

#### Scenario: Config withheld
- **WHEN** the config is being withheld because `nas` has a plain-text password
- **THEN** `status` SHALL say the config is not being backed up to the central app, and why

#### Scenario: Not enrolled
- **WHEN** `status` is run on a host that is not enrolled
- **THEN** it SHALL say that the host is not connected to a central app
