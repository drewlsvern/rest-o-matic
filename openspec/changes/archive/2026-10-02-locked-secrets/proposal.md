## Why

Repository passwords and storage credentials sit in `rest-o-matic.yaml` as plain text. That makes the config file itself a secret: it can't be copied off the host, pasted into a ticket, kept in a git repository or stored by anything else without exposing every credential in it.

This is step 2 of the central-management design ([docs/design/central-management.md](../../../../docs/design/central-management.md)). The central app will hold each host's config but must never be able to read its secrets, which requires secrets that stay encrypted everywhere except in memory on the host that uses them. It is also useful on a single host with no central app: the config becomes safe to back up and share.

## What Changes

- A repository's `password`, and any value under its `env`, can be written in a **locked** form: `password: !locked "..."`. A locked value is encrypted so that only chosen keys can open it.
- Each host has its own key, created with a new `secret keygen` command and kept outside the config. rest-o-matic unlocks a locked value in memory at the moment it starts restic, and never writes the plain text to disk.
- A locked value can also be opened by any **recovery keys** the user lists, so that losing a host does not mean losing the passwords to its backups.
- New commands under `rest-o-matic secret`:
  - `keygen` creates the host key.
  - `public-key` prints the host's public key.
  - `lock` turns a value into its locked form, ready to paste into the config.
  - `reveal` prints the plain text of a locked value from the config, for running restic by hand.
  - `check` reports whether this host can unlock every locked value in the config.
- Plain-text values keep working exactly as they do today, and can be mixed with locked ones in the same file. `password_file` and `password_command` are unchanged.
- `validate` accepts locked values without needing any key, so a config gives the same validation result on every machine.
- The locked form uses the [age](https://age-encryption.org) format, so a locked value can be opened with the stock `age` tool if rest-o-matic is unavailable.

## Capabilities

### New Capabilities
- `secrets`: the host key, the locked form of a value, who can open it, when it is unlocked, and the `secret` commands.

### Modified Capabilities
- `config`: a repository's `password` and `env` values accept the locked form; the locked form anywhere else is a config error; validation does not need a key.

## Impact

- New dependency: `filippo.io/age` (BSD-3-Clause), which brings in `golang.org/x/crypto`.
- `internal/config`: `password` and `env` values become a type that records whether the value is locked; loading rejects a locked value on any other field.
- A new `internal/secrets` package: key file handling, locking and unlocking.
- `internal/execution`: building restic's environment can now fail, when a locked value can't be opened. `backup`, `forget` and `exec` are all affected.
- `cmd/rest-o-matic`: the `secret` command group and a global `--key-file` flag.
- README and `rest-o-matic.example.yaml`: a "Secrets" section and locked examples.
- A new per-user directory holding the host key and the optional recovery recipients file.
- No change to the state file. No change for configs that don't use locked values.

Not included, and left to the enrolment change that follows: rewriting an existing config's plain-text secrets into locked form in place, and receiving the recovery key from a central app.
