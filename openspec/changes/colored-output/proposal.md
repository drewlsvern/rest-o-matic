## Why

When rest-o-matic is run by hand (`validate`, `run`, `exec`), its results are plain text, so an error, a warning and a success look identical at a glance. Config warnings (added by validate-repository-backend) are especially easy to scroll past. Colouring the severity makes the outcome readable instantly, without changing what scheduled runs write to logs.

## What Changes

- rest-o-matic's own status labels are coloured by severity: **errors red**, **warnings orange**, **successes green**. Only the label or status word is coloured, never a whole line.
- Covered output:
  - `validate`: `config error:`, `config warning:`, `config is valid`.
  - `run` and `tick` job results: `OK` / `FAILED`, and each repository's `ok` / `deferred` / `backup failed` / `forget failed`.
  - `tick`: `skipping job`, and the succeeded/failed counts in its summary line.
  - `exec`: the `rest-o-matic:` prefix on its own messages, coloured by the message's severity.
  - The top-level `Error:` prefix on command failures.
- Output from restic (exec's passthrough streams, and restic error text embedded in rest-o-matic's messages) is never coloured.
- Colour is decided separately for stdout and stderr: on only when that stream is a terminal.
- `NO_COLOR` (any non-empty value) turns colour off.
- New persistent flag `--color=auto|always|never` (default `auto`) overrides both the terminal check and `NO_COLOR`.
- With colour off, output is byte-for-byte identical to today, so cron mail, the systemd journal and scripts that parse output are unaffected.

## Capabilities

### New Capabilities
- `cli-output`: How rest-o-matic presents its own messages on a terminal: severity colours, when colour is enabled, the `--color` flag, and the guarantee that non-terminal output is unchanged.

### Modified Capabilities
<!-- none: exec's "distinguishable messages" requirement is already met by the prefix and remains unchanged; colour only reinforces it -->

## Impact

- `cmd/rest-o-matic`: every place rest-o-matic prints its own status (`validate.go`, `root.go`, `exec.go`, `exec_cmd.go`, `tick.go`), plus the root command's flag and error prefix.
- New small internal package for colour decisions and wrapping.
- Dependency: `golang.org/x/term` for terminal detection (its `golang.org/x/sys` dependency is already in the module graph).
- Depends on validate-repository-backend (PR #6), which introduces the config warnings this colours. This branch is stacked on it.
