## 1. The Contract

- [x] 1.1 `contract/checkin/v1/README.md`: endpoints, every field, the acknowledgement and resend rules, the time limits, compression, errors, and that each side ignores unknown fields
- [x] 1.2 JSON Schemas for the enrol request and reply, the check-in request and reply, and errors
- [x] 1.3 Examples: enrol request and reply, a quiet check-in, a full one, one with the config withheld, a reply asking for a resend, and a rejected credential
- [x] 1.4 Add a test-only JSON Schema validator dependency; a test that every example validates against its schema

## 2. Client and Enrolment File

- [x] 2.1 `internal/central`: message types matching the schemas
- [x] 2.2 HTTP client: system certificate store plus an optional CA file, TLS 1.2 minimum, `http://` only when allowed, proxy from the environment, `User-Agent`, bearer credential, gzip above 32 KiB, a per-request time limit
- [x] 2.3 Error mapping: `credential_rejected`, `unsupported_format`, other HTTP errors, and transport failures, each with a message a person can act on
- [x] 2.4 Enrolment file beside the host key: save owner-only, refuse when readable by others on Linux and macOS, load, remove

## 3. Enrolment Commands

- [x] 3.1 `enrol` (alias `enroll`): `--token` or a no-echo prompt, `--ca-file`, `--allow-http`, `--accept-recovery-key`, `--force`; create the host key when missing; send the request; save the enrolment with the absolute config path; print the host's name
- [x] 3.2 Recovery keys from the reply: show each new one and add it to the recovery recipients file only when confirmed or named; warn about any not added; when one is added and the config holds locked values, offer `secret relock` (or print the command without a terminal)
- [x] 3.3 `unenrol`: remove the enrolment file, keep the host key and recipients
- [x] 3.4 Tests against an in-process HTTPS server: success, token refused, already enrolled, plain HTTP refused and allowed, CA file trusted and an untrusted certificate refused, recovery key confirmed, offered without a terminal, and named

## 4. Building a Check-in

- [x] 4.1 *status* part: every job as `status <job> --json` reports it, without `generated_at` and `last_tick`, sharing the code `status` uses
- [x] 4.2 *snapshots* part: every job's recorded lists
- [x] 4.3 *config* part: the file's text, or withheld with the fields that are plain text; `env` names on the fixed list of known-harmless settings, and values marked `!plain`, may be plain
- [x] 4.4 Fingerprints; the heartbeat fields; restic's version cached by path, size and modification time
- [x] 4.5 Tests: fingerprints are stable across runs and change when content does; nothing changed gives a heartbeat only; the withheld check over plain, locked, `password_file` and `password_command` values, a harmless `env` name in plain text (sent), an unknown one (withheld), and an account identifier (withheld)

## 5. Checking In from Tick

- [x] 5.1 State: last attempt, last success, last error, acknowledged fingerprints and requested parts, updated under the state lock
- [x] 5.2 `tick`: after recording the tick time and before evaluating jobs, check in when enrolled for this config; 10 seconds without content, 60 with; never change the tick's outcome
- [x] 5.3 Acknowledge included parts on success; on a reply's `resend`, mark those parts as wanted
- [x] 5.4 Print one warning when check-ins start failing, one line when they recover, and the re-enrol message for a rejected credential
- [x] 5.5 Tests: first check-in sends everything; a quiet second one sends nothing; a finished job sends status (and snapshots) only; a failed check-in resends; a resend request is honoured; an unreachable server delays the tick by no more than the limit and its jobs still run; a different `--config` sends nothing; an outage prints exactly two lines; a rejected credential says to enrol again
- [x] 5.6 Test that real check-ins from a test config validate against the contract schemas

## 6. Check-in Command and Status

- [x] 6.1 `checkin`: one check-in now, reporting the result and which parts were sent
- [x] 6.2 `checkin --print`: the full check-in with every part, sent nowhere, working without an enrolment
- [x] 6.3 `status`: the central app line, failure details, and the config-withheld line; `checkin` in the JSON with nulls where nothing has happened
- [x] 6.4 Tests for both commands and for each `status` case, including not enrolled

## 7. Docs and Verification

- [x] 7.1 `docs/central-app.md`: enrolling (including keeping the token out of shell history, and that Fedora needs `HISTCONTROL=ignoreboth` for that), what is sent and when, what stays private, withheld configs, failures, unenrolling
- [x] 7.2 README command table and docs index; `docs/cli.md` commands and exit statuses
- [x] 7.3 Manual check: enrol against an in-process server with a private CA, tick with nothing due, let a job finish, stop the server for a few ticks, start it again; check what was sent each time and what was printed
- [x] 7.4 Run the check-in tests with restic 0.16.4 as well
- [x] 7.5 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` pass; `openspec validate enrolment-and-checkin --strict` passes
