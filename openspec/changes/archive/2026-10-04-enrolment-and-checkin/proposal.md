## Why

Each host's backups can only be checked by logging into that host, there is no view across hosts, and a host's config is saved nowhere else. The central app in [docs/design/central-management.md](../../../../docs/design/central-management.md) fixes all three, but only once hosts can tell it what they are doing. This change is that link: a host enrols with the central app once, then reports to it on every tick.

It also defines the contract between the two. The central app lives in its own repository and is written in C#, so the messages have to be written down, with schemas and examples both sides test against.

## What Changes

- **Enrolment.** In the central app you create a host and get a one-time command. Run on the host, `rest-o-matic enrol <url> --token <token>` creates the host key if there isn't one, registers the host, and stores the address and a credential beside the host key. Any recovery public key the central app offers is shown and only saved once you confirm it. `rest-o-matic unenrol` removes the enrolment.
- **A check-in on every tick.** Each `tick` starts with one request to the central app: a heartbeat carrying the host's identity, versions, and a fingerprint of each part of its state. A part is sent in full only when its fingerprint has changed since the central app last acknowledged it, or when the central app asks for it again.
- **Three parts:**
  - *status*: every job's state and run history, as `status --json` reports it;
  - *snapshots*: every job's snapshot lists;
  - *config*: the config file exactly as written. It is withheld, and `status` says why, while any repository password, or any `env` value other than a short list of known-harmless settings such as the region, is in plain text.
- **Failures never get in the way of backups.** A check-in that fails, times out or is refused never fails the tick or delays its jobs beyond a short time limit (10 seconds for a heartbeat, 60 when it carries a part). One warning is printed when check-ins start failing and one when they recover; `status` shows the detail. A rejected credential says the host must be enrolled again.
- **HTTPS only,** unless plain HTTP is explicitly allowed at enrolment for testing. Certificates are verified, against an extra certificate authority if one is given.
- **`rest-o-matic checkin`** runs one check-in now and reports what was sent. `rest-o-matic checkin --print` prints the full request without sending it, and works without being enrolled.
- **`status`** shows when the host last checked in, whether it is failing, and whether the config is being withheld.
- **The contract** is written down in `contract/checkin/v1/`: a description, a JSON Schema for each message, and example messages. The host's tests check what it actually sends against those schemas.
- The reply already has room for config versions and queued actions, which later changes will use, so adding them does not change the format.

## Capabilities

### New Capabilities
- `enrolment`: registering a host with the central app, what is stored, and removing it.
- `checkin`: when a host reports, what it sends, how changed parts are detected and acknowledged, and what happens when the central app can't be reached or refuses.

### Modified Capabilities
- `job-status`: `status` shows check-in state.

## Impact

- New `internal/central` package: the HTTP client, the messages, and the enrolment file.
- `cmd/rest-o-matic`: `enrol`, `unenrol` and `checkin` commands; `tick` checks in before running jobs.
- `internal/state`: check-in state (last attempt, last success, last error, acknowledged fingerprints, parts the central app asked for again).
- Depends on the `secret-relock` change (`secret relock` and the `!plain` marker), which comes first.
- New `contract/checkin/v1/` folder. New test-only dependency on a JSON Schema validator.
- README and docs: a new page on connecting a host to the central app.
- Nothing changes for a host that is not enrolled: `tick` makes no request at all.

Not included: config sent from the central app, queued actions, snapshot file listings, enrolment started from the host and approved in the UI, and locking values inside hook commands. The reply format leaves room for the first two.
