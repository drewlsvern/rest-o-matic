# Check-in contract, format version 1

How a rest-o-matic host talks to the central app. The host always starts
the conversation; the central app never connects to a host. This folder is
the source of truth for both sides: the JSON Schemas define each message,
the examples show them, and both repositories test against these files.

| File | Message |
|---|---|
| `enrol-request.schema.json` | Host → central: `POST /api/v1/enrol` |
| `enrol-response.schema.json` | Central → host: the 200 reply to it |
| `checkin-request.schema.json` | Host → central: `POST /api/v1/checkin` |
| `checkin-response.schema.json` | Central → host: the 200 reply to it |
| `error.schema.json` | Central → host: any error reply |
| `examples/` | One or more examples of each |

## General rules

- **HTTPS.** The host verifies the central app's certificate against the
  system's authorities, plus one extra authority if it was enrolled with
  `--ca-file`. TLS 1.2 is the minimum. Plain `http://` is used only when the
  host was enrolled with `--allow-http`, which is meant for testing. The
  host does not follow redirects.
- **JSON** bodies, UTF-8, `Content-Type: application/json`.
- **Compression.** A request body larger than 32 KiB is gzip-compressed and
  sent with `Content-Encoding: gzip`. Replies are not compressed.
- **`User-Agent`** is `rest-o-matic/<version>`.
- **Times** are RFC 3339 in UTC, to the second.
- **Format version.** Every message except errors carries
  `"format_version": 1`. It changes only for an incompatible change. Adding
  a field is not one: **each side ignores fields it doesn't know**, and the
  schemas allow them.
- **Errors.** Any reply the central app makes itself with a 4xx or 5xx
  status has the body in `error.schema.json`:

  ```json
  {"error": {"code": "credential_rejected", "message": "..."}}
  ```

  | Status | `code` | Meaning | Host does |
  |---|---|---|---|
  | 401 | `token_rejected` | The enrolment token is unknown, used or expired | Enrolment fails, nothing is stored |
  | 401 | `credential_rejected` | The credential is unknown, e.g. the host was deleted | Warns that the host must be enrolled again |
  | 400 | `unsupported_format` | The central app doesn't accept this `format_version` | Warns that the versions don't match |
  | 400 | `invalid_request` | The message doesn't match the schema | Reports the message |
  | other | any | — | Records the failure; a check-in is retried on the next tick |

  A reply without that body (a proxy's error page, say) is reported by its
  status alone.

## Enrolment

The user creates a host in the central app and gets a one-time token,
usually inside a command to paste:

```sh
rest-o-matic enrol https://backups.example.com --token 7GxK2mPq9sVw4tYb
```

The host sends `POST /api/v1/enrol` with the token, its age public key and
what it is ([example](examples/enrol-request.json)). There is no
`Authorization` header. The central app checks the token, spends it, records
the host and replies with ([example](examples/enrol-response.json)):

- `host_id`: the host's ID, opaque to the host;
- `host_name`: the name shown in the central app; the host prints it;
- `credential`: a long random secret. The central app should keep only a
  hash of it. The host sends it as `Authorization: Bearer <credential>` on
  every check-in;
- `recovery_recipients`: recovery public keys the central app offers. The
  host shows each one it doesn't already have and adds it only if the user
  confirms it. May be empty.

The host keeps the reply in an owner-only file beside its key, together
with the address, the config file it was enrolled for, and the `--ca-file`
and `--allow-http` settings.

## Check-in

Each `tick` of an enrolled host starts with one `POST /api/v1/checkin`, and
`rest-o-matic checkin` sends one on demand. The request carries the host ID,
`sent_at`, the same `host` description as at enrolment, and three
**parts**:

| Part | Content when included |
|---|---|
| `status` | `{"jobs": [...]}`: every job as `rest-o-matic status <job> --json` reports it, with its recorded runs and a summary of each snapshot list |
| `snapshots` | `{"jobs": {"<job>": {"<repository>": {"listed_at": ..., "snapshots": [...]}}}}`: every recorded snapshot list in full |
| `config` | The config file's text, exactly as written, as a JSON string |

Each part always carries a `fingerprint`: `sha256:` and the hex SHA-256 of
the content (the compact JSON encoding of `status` and `snapshots`, the
file's bytes for `config`). **`content` is null unless the part changed**:
the host includes a part only when its fingerprint differs from the last
one the central app acknowledged, or when the central app asked for it.
The central app treats fingerprints as opaque and never needs to compute
one; it compares them with what it last received.

So a host where nothing happened sends only fingerprints
([example](examples/checkin-quiet.json)); its first check-in, and the one
after a change, sends what changed ([example](examples/checkin-full.json)).
Events that change the *status* fingerprint without a backup are a job
becoming due and a job starting.

### Acknowledgement and resend

A 200 reply ([example](examples/checkin-response.json)) acknowledges every
part the request included. A failed request acknowledges nothing, so the
next check-in includes the same parts again. If two check-ins overlap the
central app may receive a part twice; it keeps the report with the later
`sent_at`.

The reply's `resend` lists parts the central app wants in full in the next
check-in whatever their fingerprints, for instance after losing its
database ([example](examples/checkin-response-resend.json)). Each reply
replaces the previous list.

The reply's `config` (always null) and `actions` (always empty) are reserved
for config sent from the central app and for queued actions in later
versions.

### Withheld config

The config is never sent while it holds a secret in plain text. In that
case `content` is null and `withheld` names each field concerned
([example](examples/checkin-config-withheld.json)):

```json
"config": {"fingerprint": "sha256:...", "content": null,
           "withheld": {"reason": "plain_text_secrets", "fields": ["repositories.offsite.password"]}}
```

`withheld` is sent on every check-in while it applies. A plain-text secret
is a repository `password` or `env` value written without `!locked`, except
an `env` value marked `!plain` or one whose name is on the host's list of
settings known not to be secret (the region, restic's tuning settings,
`TMPDIR` and the like). Locked values, `password_file`, `password_command`
and hook commands do not cause it.

### Time limits

The host gives up on a check-in after 10 seconds when it includes no part
content, and after 60 seconds when it does. A check-in never fails the
tick or stops its jobs from running; the next tick tries again.

### What the host does with errors

A failed check-in is recorded and shown by `rest-o-matic status`. The host
prints one warning when check-ins start failing and one notice when they
work again, nothing in between. `credential_rejected` is reported as "this
host must be enrolled again".
