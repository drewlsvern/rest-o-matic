## Why

A repository declared with `backend: s3` but a `url` missing the `s3:` prefix is silently treated by restic as a relative local path: `init`, `backup`, `forget`, and `exec … snapshots` all succeed against a directory on local disk, and nothing ever reaches the remote. The `backend` field is parsed but never read, so it offers no protection, and the core-restic-wrapper design's mitigation for skipping backend validation ("a misconfigured repository is caught when restic itself fails") does not hold for this misconfiguration, because restic never fails.

## What Changes

- The repository `backend` field becomes meaningful: it is checked against the scheme of `url` whenever a config is loaded for use (`validate`, `run`, `tick`, and `exec`).
- **Errors** (the command refuses to proceed) on definite contradictions:
  - `backend` is a known remote backend (`s3`, `sftp`, `rest`, `swift`, `b2`, `azure`, `gs`, `rclone`) but `url` does not begin with that backend's `<scheme>:` prefix.
  - `url` begins with a known scheme different from the one `backend` names.
  - `backend: local` but `url` begins with a known remote scheme.
- **Warnings** (printed to stderr; the command proceeds) on ambiguous cases:
  - `backend` is not a recognised value. `url` is passed through unchanged, so backends rest-o-matic doesn't know about keep working.
  - `backend: local` with a relative path, which resolves against whatever working directory rest-o-matic happens to run in.
- **BREAKING**: `backend` becomes required. A repository without it is a config error. (Every existing config already sets it.)
- `rest-server` is accepted as an alias for restic's `rest` scheme, matching the name the config spec already uses.
- `exec` now runs this repository check before invoking restic (it currently skips config validation entirely).
- No auto-prefixing: `url` remains exactly the string passed to `restic -r`.
- The example config stops calling `backend` "informational".

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `config`: The Repository Declaration requirement changes from "accepted without any special-casing based on backend type" to requiring `backend` and checking it against the `url` scheme, with errors for contradictions and warnings for unknown backends and relative local paths.
- `restic-exec`: Repository-Anchored Invocation additionally rejects a target repository whose backend/url check fails, before any restic process is started.

## Impact

- `internal/config`: new backend/scheme check, a way to return warnings alongside errors from validation, and the `rest-server` alias.
- `cmd/rest-o-matic`: `validate`, `loadAndValidate` (used by `run`/`tick`), and `exec` print warnings and honour the new errors.
- `rest-o-matic.example.yaml` and any README config docs: update the `backend` comment and document the accepted values.
- Existing configs: any with a mismatched prefix (exactly the silent-local-repo case) or a missing `backend` will stop running until fixed. A relative local path will only produce a warning.
