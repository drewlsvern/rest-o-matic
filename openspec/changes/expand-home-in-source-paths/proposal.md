## Why

A source path written as `~/documents` passes `validate` and then fails at backup time with "does not exist". rest-o-matic hands paths to restic with no shell in between, so nothing expands the `~`. People expect it to work, and the project's own README and example config showed it until recently.

## What Changes

- A source path that is `~`, or starts with `~/`, has the `~` replaced by the home directory of the user running rest-o-matic, when the config is loaded.
- If the home directory can't be determined, loading the config fails with an error naming the job and the path, instead of a backup failing later.
- Only a leading `~` followed by a separator is expanded. `~alice/...`, a `~` elsewhere in a path, and environment variables are left exactly as written.
- Only source paths are affected. A repository `url` is still passed to restic unchanged, as the `config` spec requires.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `config`: source paths gain home-directory expansion.

## Impact

- `internal/config`: `Load` expands source paths after decoding.
- README and `docs/configuration.md`: stop saying `~` is not expanded, and note whose home directory is used.
- Behaviour change only for configs that use `~` in a source path, which did not work before. Under `sudo`, `~` follows the `HOME` that `sudo` leaves in place.
