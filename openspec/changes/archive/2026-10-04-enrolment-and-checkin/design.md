## Context

- `tick` loads and validates the config, checks the state directory, records the tick time, then runs due jobs ([tick.go](../../../../cmd/rest-o-matic/tick.go)). There is no HTTP client anywhere in the project yet.
- `status` builds one report struct from config, state, lock probes and snapshot lists ([status.go](../../../../cmd/rest-o-matic/status.go)). The *status* and *snapshots* parts are built from the same code, so they can't drift from what `status --json` shows.
- The host key and recovery recipients live in the user's config directory ([secrets.go](../../../../internal/secrets/secrets.go)). The enrolment belongs beside them: it identifies the user on the host, like the key.
- State updates go through one locked read-modify-write ([state.go](../../../../internal/state/state.go)).
- The central app is Blazor Server behind Caddy on a tailnet, in another repository. It can't import Go types.

See proposal.md for motivation, and docs/design/central-management.md for the wider plan.

## Goals / Non-Goals

**Goals:**
- A host can be connected in one pasted command.
- A quiet minute costs a few hundred bytes; only what changed is sent.
- Nothing about the central app can stop, slow down much, or fail a backup.
- The contract is precise enough for a C# implementation to be written from it alone.
- The format already has room for config delivery and actions.

**Non-Goals:**
- Config from the central app, actions, and file listings of snapshots. Later changes; the reply format reserves their place.
- Enrolment started from the host and approved in the UI.
- Deciding when a host is overdue. That is the central app's call; the host neither knows nor reports how often it is meant to check in.
- Locking values inside hook commands.
- Compressing responses, retries within a tick, or a queue of unsent reports. The next tick is the retry.

## Decisions

### Enrolment

```
UI:    create host "prd-podman-01"  → one-time command with a token
host:   rest-o-matic enrol https://backups.example.com --token 7Gx…
          host key created if missing
          POST /api/v1/enrol
central: token checked and spent, host recorded
          reply: host ID, credential, recovery public keys
host:   each new recovery key shown, saved only if confirmed
        enrolment saved beside the host key
```

Request and reply:

```json
POST /api/v1/enrol
{
  "format_version": 1,
  "token": "7Gx…",
  "public_key": "age1…",
  "host": {"hostname": "prd-podman-01", "os": "linux", "arch": "amd64",
           "rest_o_matic_version": "v0.2.0", "restic_version": "0.17.3"}
}

200
{
  "format_version": 1,
  "host_id": "8f2c…",
  "host_name": "prd-podman-01",
  "credential": "rom1_…",
  "recovery_recipients": ["age1…"]
}
```

The token is the one-time value from the UI. The credential is a long random value the central app generates and stores only a hash of. The host sends it as `Authorization: Bearer` on every check-in.

The enrolment is saved as `enrolment.json` beside the host key, owner-only, and refused when readable by others on Linux and macOS, as the key is:

```json
{
  "url": "https://backups.example.com",
  "host_id": "8f2c…",
  "host_name": "prd-podman-01",
  "credential": "rom1_…",
  "config_path": "/home/podman-runner/backups/rest-o-matic.yaml",
  "ca_file": null,
  "allow_http": false,
  "enrolled_at": "2026-10-03T09:12:00Z"
}
```

`config_path` is what makes a tick with a different `--config` stay silent, so a test config never reports as the real host.

A request signed with a key held only by the host was considered instead of a bearer credential. The host's age key can encrypt but not sign, so it would need a second key. Over verified HTTPS a bearer credential is enough, and revoking it is deleting the host in the UI.

### The check-in

```json
POST /api/v1/checkin
Authorization: Bearer rom1_…

{
  "format_version": 1,
  "host_id": "8f2c…",
  "sent_at": "2026-10-03T09:15:00Z",
  "host": {"hostname": "prd-podman-01", "os": "linux", "arch": "amd64",
           "rest_o_matic_version": "v0.2.0", "restic_version": "0.17.3"},
  "parts": {
    "status":    {"fingerprint": "sha256:9b1…", "content": null},
    "snapshots": {"fingerprint": "sha256:44c…", "content": null},
    "config":    {"fingerprint": "sha256:e07…", "content": null, "withheld": null}
  }
}
```

That is the whole of a quiet minute: under a kilobyte. When a part is included, `content` holds it:

- **status**: `{"jobs": [...]}`, each job exactly as `status <job> --json` reports it (with `runs` and `snapshot_lists` summaries), and without `generated_at` and `last_tick`. Those two change every tick and would make the fingerprint useless; `sent_at` carries the same information.
- **snapshots**: `{"jobs": {"<job>": {"<repository>": {"listed_at": ..., "snapshots": [...]}}}}`, the recorded lists with every snapshot.
- **config**: the config file's text, as a string, byte for byte. Comments and YAML anchors survive.

When the config is withheld, `content` is null and `withheld` says why:

```json
"config": {"fingerprint": "sha256:e07…", "content": null,
           "withheld": {"reason": "plain_text_secrets",
                        "fields": ["repositories.nas.password", "repositories.offsite.env.AWS_SECRET_ACCESS_KEY"]}}
```

The reply:

```json
200
{
  "format_version": 1,
  "server_time": "2026-10-03T09:15:00Z",
  "resend": [],
  "config": null,
  "actions": []
}
```

`resend` lists parts the central app wants in full next time, for example after restoring its database. `config` and `actions` are always null and empty in this version; later changes fill them. A host ignores fields it doesn't know.

Errors carry `{"error": {"code": "...", "message": "..."}}`:

| Status | `code` | Host does |
|---|---|---|
| 401 | `token_rejected` | At enrolment: fails, saying the token is unknown, used or expired; nothing is stored |
| 401 | `credential_rejected` | Warns that the host must be enrolled again |
| 400 | `unsupported_format` | Warns that the versions don't match |
| Anything else, or no reply | — | Records the failure; tries again next tick |

### Fingerprints and acknowledgement

A part's fingerprint is `sha256:` and the hex SHA-256 of its content: the JSON encoding for *status* and *snapshots*, the file's bytes for *config*. Go's encoder writes struct fields in a fixed order and map keys sorted, so the same content always gives the same fingerprint. The central app treats fingerprints as opaque; it compares what it last received and never has to recompute one.

The state file gains, under `checkin`, the fingerprint the central app last acknowledged for each part and the parts it asked for again. A part's content is included when its fingerprint differs from the acknowledged one, or it was asked for. On a successful reply, each included part is recorded as acknowledged. A failed check-in records nothing, so the next one sends the same parts again.

Overlapping ticks may both send a part. That is harmless: the central app keeps whichever report has the later `sent_at`.

Two things change the *status* fingerprint without a backup: a job becoming due at its schedule boundary, and a job starting (it becomes `running`). Both are worth reporting, and each happens a handful of times a day per job.

### Where it sits in a tick

```
tick: load config → check state directory → record tick time
      → check in (if enrolled for this config)
      → evaluate and run due jobs
```

The report describes the state before this tick's jobs. A job that finishes is reported by the next tick, within a minute. A tick that waits two hours for a busy repository doesn't block reporting, because later ticks keep checking in.

### Time limits

10 seconds for a check-in that carries no part content; 60 seconds when it carries any. The common case can only ever delay jobs by a few seconds, even if the central app silently drops connections. A first check-in carrying every snapshot list over a slow link still has time to finish. If one doesn't, it isn't acknowledged and is sent again next tick.

Request bodies larger than 32 KiB are sent gzip-compressed with `Content-Encoding: gzip`. ASP.NET Core accepts that with its built-in request decompression.

### HTTPS

The client uses the system's certificate store, plus the authority in `ca_file` if set. TLS 1.2 is the minimum. `http://` is refused unless `allow_http` was set at enrolment. Proxies from the environment (`HTTPS_PROXY`) are honoured. The `User-Agent` is `rest-o-matic/<version>`.

### Withholding the config

The config is withheld while any repository has a plain-text `password`, or a plain-text `env` value whose name is not on a fixed list of settings known not to be secret:

| Allowed in plain text | Why |
|---|---|
| `AWS_DEFAULT_REGION`, `AWS_REGION` | Where the bucket is |
| `RESTIC_COMPRESSION`, `RESTIC_PACK_SIZE`, `RESTIC_READ_CONCURRENCY` | restic's own tuning |
| `RESTIC_CACHE_DIR`, `TMPDIR`, `GOMAXPROCS` | Paths and process settings |

A value written `!plain` (from the `secret-relock` change) is also allowed: that is the user saying explicitly that this one is not secret. Every other name counts as a secret, so a variable rest-o-matic doesn't recognise can never be uploaded readable by mistake. That includes account identifiers such as `AWS_ACCESS_KEY_ID`, `B2_ACCOUNT_ID` and `AZURE_ACCOUNT_NAME`: often treated as public, but they identify an account and are better locked. The list lives in one place in the code and can grow when a harmless variable turns out to be missing from it.

Secrets stay write-only, as decided in the architecture: the central app can replace a locked value (the browser locks the new one for the host's and the recovery keys) but never read it. A harmless setting is kept off that path by leaving it in plain text.

The check runs on the parsed config. `password_file` and `password_command` hold no secret and don't count. Hook commands are sent as written.

### Recovery keys added at enrolment

A value locked before a recovery key existed can't be opened with it. When enrolment adds a recovery key that wasn't listed before, and the config holds locked values, it says so and offers to re-lock them using `secret relock` (from the `secret-relock` change, which this one depends on). It has what it needs: the host's own key opens the values, and the new key is in the recipients file. Without a terminal it prints the command instead. rest-o-matic can't tell from a locked value which keys it was locked for, so the offer is made whenever a new recovery key is added and there are locked values, not only when it is strictly needed.

### restic's version

Every check-in carries restic's version, which means running `restic version`. The result is cached in the state file with restic's path, size and modification time, so it runs again only when restic is replaced.

### Warnings

The state records the last attempt, the last success and the last error. A check-in that fails after a success prints one warning to standard error; one that succeeds after failures prints one line saying check-ins are working again. Nothing else is printed about check-ins, so an outage costs two lines of output, not one per tick.

### `status`

```
last tick: just now
central app: https://backups.example.com, last check-in just now
```

```
central app: https://backups.example.com, last check-in 3 hours ago
  failing since 3 hours ago: dial tcp 100.64.0.5:443: connection refused
config not backed up to the central app: repositories.nas.password is in plain text
```

The JSON gains `checkin`: `enrolled`, `url`, `host_id`, `host_name`, `last_attempt`, `last_success`, `last_error`, and `config_withheld` (the fields, or null).

### Commands

| Command | Does |
|---|---|
| `rest-o-matic enrol <url> [--token T] [--ca-file F] [--allow-http] [--accept-recovery-key K]... [--force]` | Enrols this host; `enroll` works too |
| `rest-o-matic unenrol` | Removes the enrolment; keeps the host key |
| `rest-o-matic checkin` | Checks in now and says which parts were sent |
| `rest-o-matic checkin --print` | Prints the full check-in, every part included, without sending |

### The contract folder

```
contract/checkin/v1/
  README.md                     the exchange, in prose: endpoints, rules, errors
  enrol-request.schema.json
  enrol-response.schema.json
  checkin-request.schema.json
  checkin-response.schema.json
  error.schema.json
  examples/
    enrol-request.json  enrol-response.json
    checkin-quiet.json  checkin-full.json  checkin-config-withheld.json
    checkin-response.json  error-credential-rejected.json
```

The host's tests validate every example, and real messages built from a test config and state, against the schemas. The UI repository validates its own messages against the same files. A test-only dependency on a JSON Schema validator for Go is added.

### Testing without a central app

Tests run an HTTPS server in-process (`httptest.NewTLSServer`) that records what it receives and replies as told. Its certificate is passed as `--ca-file`, which also tests that path.

## Risks / Trade-offs

- **[Risk] A stolen credential** lets someone send false reports for that host, and later fetch its config (which holds no readable secrets). → It is owner-only on the host, stored only as a hash centrally, and revoked by deleting the host in the UI.
- **[Risk] Reports carry file paths, error text, hostnames and hook commands in plain text.** → Stated plainly in the docs. Secrets are never sent readable: the config is withheld while any is in plain text.
- **[Trade-off] Strict withholding** forces harmless `env` values to be locked before the config is backed up.
- **[Trade-off] A silently unreachable central app adds up to 10 seconds to every tick.** Jobs still start well within their schedule.
- **[Risk] Host clocks disagree with the central app.** → The central app uses its own time for "last seen" and overdue. `sent_at` only orders reports from one host.
- **[Trade-off] Two code bases implement one format.** → Schemas, examples and tests on both sides.
- **[Risk] A token pasted into a shell may land in its history.** → It works once and expires. On Fedora a leading space does not keep it out of history unless `HISTCONTROL` is set; the docs say how.

## Migration Plan

Nothing changes until a host is enrolled. To roll back, `rest-o-matic unenrol`, or install an older binary, which ignores the enrolment file and the new state fields.

## Open Questions

- The token's lifetime and the credential's format are the central app's to choose. The host treats both as opaque strings.
