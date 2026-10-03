## Why

A locked value can only be opened by the keys it was locked for. When that set of keys changes, every value has to be opened with a key you still have and locked again for the new set. That happens when a lost host is replaced (its key is gone), when a recovery key is added after values were locked (they don't include it), and when a key is replaced. Today that means `secret reveal` and `secret lock` for each value, pasting each result back into the config by hand, with the plain text passing through the terminal every time.

Separately, the coming check-in change withholds a config from the central app while any `env` value is in plain text, unless its name is on a short built-in list of harmless settings. A user's own harmless variable needs a way to say "this one isn't secret".

## What Changes

- **`rest-o-matic secret relock`** opens every locked value in the config and locks it again for this host's key and every key in the recovery recipients file, then rewrites the config. Only the locked values change; comments, YAML anchors, quoting and everything else in the file stay byte for byte the same.
  - `--with-key <file>` opens the values with a different key, such as the recovery key on a replacement host.
  - `--dry-run` reports what would be re-locked without writing.
  - If any value can't be opened, nothing is written and each one is listed.
- **`!plain`** marks an `env` value as deliberately not secret: `MY_BUCKET_PREFIX: !plain "host-a/"`. It behaves exactly like a plain value; it only records the decision. It is accepted only on `env` values.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `secrets`: re-locking every locked value for a new set of keys.
- `config`: the `!plain` marker on `env` values.

## Impact

- `cmd/rest-o-matic`: the `relock` subcommand of `secret`.
- `internal/config`: the `!plain` tag, and finding every locked value in the file.
- `docs/secrets.md`: re-locking, replacing a lost host, adding a recovery key later, and `!plain`.
- No change for a config that uses neither.
