## Context

- A repository's credentials are plain strings: `password`, and a map of `env` values ([types.go](../../../internal/config/types.go)). `password_file` and `password_command` point at a secret without containing it.
- `repoEnv` turns those into restic's environment (`RESTIC_PASSWORD`, plus each `env` entry) and can't fail ([restic.go](../../../internal/execution/restic.go)). It is used by `backup`, `forget` and `exec`.
- `config.Load` unmarshals YAML straight into the config types; `config.Validate` then checks cross-references. `validate` runs both and nothing else.
- Configs use YAML anchors and merge keys, which must keep working.
- `golang.org/x/term` is already a dependency (used for colour detection), so a no-echo prompt needs nothing new.

See proposal.md for motivation, and docs/design/central-management.md for how this is used later.

## Goals / Non-Goals

**Goals:**
- A config whose secrets are all locked is safe to copy anywhere.
- Plain-text configs are untouched: no migration, no new required step.
- A host that is lost does not take its repository passwords with it, if a recovery key was listed.
- Locked values remain usable without rest-o-matic, through the stock `age` tool.
- `validate` gives the same answer on every machine, which the central app will rely on.

**Non-Goals:**
- Protecting secrets from someone who can already read files as the backup user on the host. They can read the host key too. Locking protects the config once it leaves the host.
- Rewriting an existing config's plain-text secrets into locked form in place. That needs comment- and anchor-preserving YAML edits and belongs to the enrolment change.
- Fetching a recovery key from a central app, generating repository passwords, or rotating keys.
- Locking anything other than `password` and `env` values. A `url` with embedded credentials can't be locked, because validation has to read it; put those credentials in `env`.
- Making locked values available to hooks.

## Decisions

### The locked form is a YAML tag
```yaml
repositories:
  offsite:
    backend: s3
    url: "s3:https://s3.example.com/bucket/restic/prd-podman-01"
    password: !locked "YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+IFgyNTUxOSA..."
    env:
      AWS_ACCESS_KEY_ID: AKIA...
      AWS_SECRET_ACCESS_KEY: !locked "YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+..."
```
The key keeps its name, so nothing else about the schema changes, and a tag can't be confused with a real password the way a `locked:` prefix inside the string could. A separate key (`password_locked:`) was considered; it doubles every credential field and doesn't work for arbitrary `env` names.

The value is the base64 encoding of a binary age file, on one line. age's own ASCII armor is multi-line, which is awkward as a YAML scalar.

### `password` and `env` values become a `Secret` type
`config.Secret` holds the text and whether it was tagged `!locked`, set by a custom `UnmarshalYAML`. Code that needs the plain text must go through an unlock step, so a locked value can't be passed to restic by mistake.

A locked tag on any other field is caught by walking the parsed YAML node tree before decoding: any `!locked` node whose path is not `repositories.<name>.password` or `repositories.<name>.env.<NAME>` is an error with its line number. Relying on the decoder is not enough, since it would silently accept a tagged scalar for a plain string field. The walk follows aliases, so a locked value shared through an anchor is allowed where its use is.

### age, generating X25519 keys
`filippo.io/age` is the reference implementation of the age format, written by its designer. It is small, has one obvious way to do things, and its file format is stable and documented. A locked value lists several recipients natively, which is exactly "the host plus recovery keys".

Alternatives considered:

- **NaCl box from `golang.org/x/crypto`.** Sound primitives, but several recipients, the key file format and the encoding would all be our own design, and no existing tool could open the result.
- **OpenPGP.** The `x/crypto` implementation is deprecated, and the format is far larger than this needs.
- **SOPS.** It solves the same problem for whole files and itself uses age keys. It encrypts every value in a file and adds a file-wide integrity check, which doesn't fit a config where only a few values are secret and the rest must stay readable and editable.

`secret keygen` creates an X25519 key. Since v1.3 the library recommends its newer hybrid post-quantum keys for most uses, but each hybrid recipient adds well over a kilobyte to every locked value, which is unwieldy in a YAML file, and the stock `age` tool only understands them from v1.3. With X25519, a value locked for two keys is about 400 characters. Loading accepts either kind, so a user who wants a hybrid key can create one with `age-keygen -pq` and use it as the host key or a recipient.

age encrypts but does not sign: anyone holding a public key can produce a locked value for it. That is fine here, because whoever can edit the config can already change what the host does.

The host key file is what `age-keygen` writes, and the recovery recipients file is what `age -R` reads, so both are interchangeable with the stock tools:

```sh
echo '<locked value>' | base64 -d | age --decrypt -i ~/.config/rest-o-matic/host.key
```

### The key lives in the user's config directory, not the state directory
Default: `<user config dir>/rest-o-matic/host.key`, with the recovery recipients beside it as `recovery-recipients`. On Linux that is `~/.config/rest-o-matic/`. `--key-file` overrides the key path, and the recipients file is looked for in the same directory.

The state directory was the first idea and was rejected on closer reading:

- State is disposable. The documented fix for a damaged state directory is to delete its files. Deleting the key would make every locked value unreadable on that host.
- `--state-dir` defaults to `.rest-o-matic` relative to the working directory, so two working directories would mean two keys.
- A key identifies the user on the host, not one config. One key serving every config that user runs is the natural unit, and it is where the later enrolment credential belongs too.

The directory is created `0700` and the key `0600`. On Linux and macOS a key readable by group or others is refused, as ssh does, because a silently over-shared key defeats the feature. Windows has no equivalent check.

### Recipients: the host, the recovery file, and `--recipient`
`secret lock` encrypts to the union of:

1. the host's own public key, if a host key exists;
2. every key in `recovery-recipients`, if the file exists;
3. every `--recipient`.

Putting recovery keys in a file beside the host key means every `secret lock` on that host includes them without anyone remembering to. The enrolment change will write that file; until then the user can create it by hand with their own recovery public key.

A host key is not required to lock: an operator can lock a value on their workstation for a server's public key with `--recipient`.

### Unlock lazily, where restic's environment is built
`repoEnv` gains an unlocker and can return an error. It is called once per restic invocation, so a locked value is opened only for the repository in use, and only when restic is about to start. The host key is read at most once per process.

Unlocking everything at load time was rejected: `exec nas` would fail because some unrelated repository was locked for another host, and `validate` would need a key.

An unlock failure is reported as that repository's backup error. Within a job it is one failed repository, consistent with how an unreachable repository is treated today.

### `validate` checks form only; `secret check` checks the key
`validate` confirms a locked value is base64 that decodes to something starting with the age header. It never looks for a key, so its result doesn't depend on the machine.

`secret check` answers the other question: can this host open every locked value in this config? It is the thing to run after copying a config to a host, or after replacing a host key.

### `secret reveal` addresses values by repository, not by pasting
`secret reveal offsite` and `secret reveal offsite AWS_SECRET_ACCESS_KEY` read the config. That avoids copying a long locked string onto a command line and matches how `exec` names repositories. It refuses a plain-text value instead of echoing it, so its meaning stays "unlock", and a typo in the name is an error, not silence.

### `secret lock` input
Standard input when it is not a terminal (one trailing newline is dropped), otherwise a no-echo prompt asked twice. The value is never taken from a command-line argument, which would leave it in shell history and the process list. Output is exactly the text to paste after the key: `!locked "..."`.

## Risks / Trade-offs

- **[Risk] Losing the host key with no recovery key makes locked values unreadable.** → `secret keygen` and `secret lock` both say so when no recovery recipients file exists. The README puts creating a recovery key first.
- **[Risk] A locked value is long,** about 400 characters for two recipients, growing with each one. → Acceptable for a handful of values per config. YAML anchors still work for a value shared by several repositories.
- **[Risk] The plain text is still in restic's environment** while it runs, as it is today. → Unchanged; out of scope.
- **[Trade-off] New dependency.** `filippo.io/age` and `golang.org/x/crypto`. Both are pure Go, so the cgo-free cross-compiled release is unaffected.
- **[Trade-off] `repoEnv` can now fail,** which touches every caller. → The change is mechanical, and the alternative (unlocking earlier) has the worse behaviour described above.
- **[Risk] A user locks a value for the wrong key and finds out at backup time.** → `secret check`, and the unlock error names the repository, the field and the reason.

## Migration Plan

Nothing to migrate. Existing configs keep working. To adopt: run `secret keygen`, optionally create the recovery recipients file, then replace values with the output of `secret lock`. Rolling back means putting plain-text values back; an older binary rejects the `!locked` tag as a parse error or passes the locked text to restic as a wrong password, so don't run an older binary against a config with locked values.

## Open Questions

- Whether `secret keygen` should offer to create a recovery key as well. It can be added later without changing anything here; for now the README shows how with `age-keygen`.
