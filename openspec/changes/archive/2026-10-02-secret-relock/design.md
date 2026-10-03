## Context

- A locked value is an age file, base64-encoded onto one line after the `!locked` tag ([secrets.go](../../../../internal/secrets/secrets.go)). Locking the same value twice gives different text.
- `config.Load` parses the YAML into a node tree, checks where `!locked` appears, then decodes ([load.go](../../../../internal/config/load.go)). Each `password` and `env` value is a `config.Secret` that records whether it was locked.
- The age format does not record which keys a value was locked for, so the only way to know whether a key opens a value is to try it.

## Goals / Non-Goals

**Goals:**
- One command to move every locked value to the current set of keys.
- The config file comes back identical apart from the locked values.

**Non-Goals:**
- Locking plain-text values in place. That is a different job (it changes which values are locked) and belongs with enrolling an existing host.
- Choosing which keys to lock for. It is always this host's key plus the recovery recipients, the same set `secret lock` uses.

## Decisions

### Replace the text, don't re-encode the YAML
Re-encoding a modified node tree with the YAML library loses blank lines and can change indentation, quoting and the layout of inline maps. Instead `relock` finds each locked value's text in the file and replaces just that text with the new locked value, then writes the result.

A locked value's text is long, random base64, so it appears in the file only where it was written. The values are found by walking the parsed node tree for `!locked` scalars, including ones defined under an anchor; an alias reuses its anchor's node, so each value is opened and replaced once however many places refer to it. If the same locked text has been pasted in two places, both get the same new value, which is correct because they hold the same secret.

As a check, the rewritten file is parsed again and every locked value is opened with the host key before anything is written. If that fails, nothing is written.

### All or nothing
Every value is opened before any is replaced. A file with some values moved to the new keys and some not would be the worst outcome: it would look fine until the wrong key was needed.

### Written atomically, mode kept
A temporary file in the same directory, renamed over the original, with the original's permissions.

### `!plain` is a marker, not a type
`config.Secret` gains a flag for "marked plain". Everything that uses the value treats it like plain text. The placement check that already confines `!locked` to `password` and `env` values confines `!plain` to `env` values only, since a password is by definition a secret. The flag exists so that the check-in change can tell "the user says this isn't secret" from "nobody said".

## Risks / Trade-offs

- **[Risk] The recovery key is typed or copied onto a replacement host** for `--with-key`. → The docs say to delete it from that host afterwards; the command doesn't need it again.
- **[Trade-off] Re-locking changes every locked value's text,** so the config shows as changed even though no secret did. → Expected; the central app will see one new config version.
- **[Risk] A locked value that is also used in a hook command,** pasted as text. → Only `!locked` scalars are re-locked; text inside a hook is never touched. Hooks can't use locked values anyway.

## Migration Plan

Nothing to migrate.
