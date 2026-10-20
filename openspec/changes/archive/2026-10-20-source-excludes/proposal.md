## Why

A job backs up everything under its source paths, with no way to leave part of a path out. A job backing up `~/.local` to keep application settings also picked up 47 GB of rootless Podman storage and 7 GB of Trash. Over a slow upload link, that turned a config backup into one that would have taken days. Today the only workaround is to list every wanted subfolder. That silently misses any folder an application adds later, which is the opposite of what a backup should do.

restic already supports this. rest-o-matic just needs to let a job say it.

## What Changes

- **`source.exclude`**: a list of restic exclude patterns, passed to `restic backup` as `--exclude` in the order written. restic's pattern syntax is used unchanged: an absolute path excludes that path, a bare name matches anywhere in the tree, and `**` matches any depth. A leading `~` is expanded, exactly as it is for source paths, because restic does not expand it and an unexpanded `~/...` pattern would silently exclude nothing.
- **`source.exclude_caches`** (default `false`): passes `--exclude-caches`, skipping directories marked with a valid `CACHEDIR.TAG`.
- **`source.one_file_system`** (default `false`): passes `--one-file-system`, so restic doesn't cross into other mounts (autofs or network mounts, or the overlay mounts of running rootless containers) found under a source path.
- **Validation**: a pattern that isn't valid glob syntax, or a value of the wrong type, is an error. An absolute exclude outside every source path of its job (so it can never match), and one that excludes a whole source path, are warnings.
- **The source check tells types from modifiers.** `source` currently treats every key other than `paths` as an unsupported source type. The new keys modify a source; they are not types. `container` and other unknown keys are still rejected as before.
- **Deferred, decided:** `exclude_larger_than` is left out. restic skips files over the limit with no message and no record in the snapshot, and it rejects a size like `15GB` only once the backup is running, after the before hooks have run. If it is added later, it takes a size (`exclude_larger_than: 15G`), leaving it out means off, and `validate` checks the size.
- **Not in this change:** case-insensitive patterns (`--iexclude`), `exclude_file`, `exclude_if_present`, glob includes, and showing a snapshot's excludes in `status`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities
- `config`: `source` accepts `exclude`, `exclude_caches` and `one_file_system`; `~` expansion extends to exclude patterns; new validation errors and warnings for excludes.
- `backup-execution`: `restic backup` is given the job's excludes and flags.

## Impact

- `internal/config`: `Source` gains three fields; its unmarshaller separates source types from modifiers; `Load` expands `~` in excludes; new checks in `validate.go`; `config.schema.json` gains the three keys.
- `internal/execution`: `ResticRunner.Backup` takes the source's exclude options and adds them to the restic arguments.
- Docs: `docs/configuration.md`, and the `source` section of `rest-o-matic.example.yaml`.
- Central app: the config travels to central as text, so the check-in contract does not change. The editor picks up the keys from `rest-o-matic schema`. Note this in the UI repo handoff.
- Existing configs behave exactly as before: every new key is optional and off by default. The snapshot's parent is still found when excludes are added or changed (checked with restic 0.19.1), so the first run after adding excludes doesn't rescan everything.
