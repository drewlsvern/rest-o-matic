## Why

rest-o-matic currently has no way to tell which build you're running — no `--version` flag, no version shown anywhere. That's already awkward during solo development ("did I rebuild after that last change?") and will matter more once the project is public and other people are running builds they downloaded or built themselves.

## What Changes

- Add version reporting sourced from Go's automatic VCS/module build-info stamping (`runtime/debug.ReadBuildInfo()`), not a hardcoded constant and not an `-ldflags`-injected value. This requires no build tooling changes and correctly reflects every stage of the project's lifecycle with no code changes needed later: an untagged pseudo-version now, the exact tag once a release is cut and built at that commit, and a "past last tag" pseudo-version for dev builds in between — including an automatic `+dirty` suffix when the working tree has uncommitted changes.
- Running `rest-o-matic` with no arguments SHALL show a short version line (just the version string) above the existing command listing, which is otherwise unchanged.
- The `--version` flag (cobra's auto-generated flag, enabled by setting `rootCmd.Version`) SHALL show a richer, multi-line build-info block: version, full commit hash, commit time, dirty-tree flag, and the Go version used to build.
- No new subcommand is added — `--version` is the one place for the detailed block, since a separate `version` subcommand would only duplicate it.

**Explicitly out of scope:** hardcoded version constants, `-ldflags`/Makefile/build-script changes, any new subcommand, any change to existing commands' (`validate`/`run`/`tick`/`exec`) behavior or output, and handling the case of building from a source archive with no `.git` directory (accepted limitation, noted in design.md, not solved here).

## Capabilities

### New Capabilities
- `cli-version-info`: Reporting the running build's version, both as a short line on the no-args invocation and as a detailed multi-line block via `--version`, sourced entirely from Go's automatic build-info stamping.

### Modified Capabilities
(none — this is purely additive and does not change any existing capability's requirements)

## Impact

- Touches only the CLI entry point (`cmd/rest-o-matic/root.go`, `cmd/rest-o-matic/main.go`) — no changes to `internal/config`, `internal/execution`, `internal/lock`, `internal/schedule`, or `internal/state`.
- No new external dependencies (uses only the standard library's `runtime/debug`, plus cobra's existing `Version` field support already in use).
- No change to any existing command's flags, behavior, or output.
