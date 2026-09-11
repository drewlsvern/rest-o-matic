## Context

See proposal.md for motivation. This is confined entirely to `cmd/rest-o-matic` — no other package needs changes. The relevant spec is `specs/cli-version-info/spec.md` in this change.

During the explore-mode discussion that produced this proposal, the approach below was verified empirically (built and ran throwaway test binaries, and read cobra v1.10.2's actual source) rather than assumed from memory. That verification is summarized here since it's the basis for every decision in this document.

## Goals / Non-Goals

**Goals:**
- Version reporting that requires zero build-process changes now, and continues to work with no code changes once the project starts cutting tagged releases.
- A short version line on no-args invocation; a detailed multi-line block on `--version`.

**Non-Goals:**
- Handling a build from a source archive with no `.git` directory (accepted limitation - see Risks).
- Any new subcommand.
- Any change to `validate`/`run`/`tick`/`exec`.

## Decisions

### Version source: `runtime/debug.ReadBuildInfo()`, not ldflags, not a hardcoded constant
Verified directly: a plain `go build` from within this project's git working tree already embeds everything needed, readable at runtime via `debug.ReadBuildInfo()`:

- `bi.Main.Version` - a Go-computed pseudo-version when there's no tag at the current commit (e.g. `v0.0.0-20260910172449-95b2808800b6`), the exact tag when built at a tagged commit (e.g. `v1.0.0`), or a "next version, pseudo" form for a dev build after a tag (e.g. `v1.0.1-0.20260910172510-82aaf2754163`) - confirmed for all three cases by building real test binaries at each stage. Go appends `+dirty` to this value itself when the working tree has uncommitted changes; nothing needs to be added for that.
- `bi.Settings` - a `[]debug.BuildSetting`, containing (when built from git) `vcs.revision` (full commit hash), `vcs.time` (commit timestamp), and `vcs.modified` (dirty-tree boolean) as separate key/value entries alongside `Main.Version`.
- `bi.GoVersion` - a direct field on the same struct giving the Go toolchain version used to build.

This was chosen over the classic `-ldflags "-X main.version=..."` pattern (used by many Go CLIs, including restic) specifically because it needs no build script to exist or be invoked correctly - `go build ./cmd/rest-o-matic` alone is sufficient, both today and after the project starts tagging releases. It was chosen over a hardcoded constant because that requires manual bumping and inevitably drifts from what was actually built.

### Two independent display surfaces, confirmed via cobra's own source
Read directly from `github.com/spf13/cobra@v1.10.2/command.go`: `defaultUsageTemplate` and `defaultHelpTemplate` (which back the plain no-args output) contain no `{{.Version}}` reference at all; `{{.Version}}` appears only in the separate `defaultVersionTemplate`, rendered when the auto-generated `--version`/`-v` flag (enabled by setting `rootCmd.Version`) is invoked. So the two surfaces need separate, small pieces of wiring - they are not automatically linked by cobra:

- **No-args short line**: since cobra's Version field is a single string and the no-args templates never reference it, the short version line is produced by folding it into the root command's own description output (e.g. via its `Short`/`Long` field, computed once at startup) rather than by fighting cobra's template engine. The existing `Available Commands`/`Flags` listing cobra already prints is untouched.
- **`--version` rich block**: `rootCmd.Version` is set to the full, pre-formatted multi-line block (version + commit + commit time + dirty + Go version) built once at startup from the same `debug.BuildInfo` read. `rootCmd.SetVersionTemplate` is adjusted so cobra prints that block as-is rather than wrapping it in its default one-line `"<name> version <value>"` phrasing.

Both the short line and the rich block are built from one `debug.ReadBuildInfo()` call made once (e.g. in `cmd/rest-o-matic/root.go`'s `init()` or at the start of `main()`), not re-read per invocation of either surface.

### No separate `version` subcommand
Since `--version` already carries the full detailed block, a `version` subcommand would only duplicate it. Not added.

## Risks / Trade-offs

- **[Risk]** This entire approach depends on `.git` being present at build time. Building from a source archive with no `.git` directory (e.g. a GitHub "Download ZIP") will fall back to whatever Go's default is with no VCS settings present - likely just `Main.Version == "(devel)"` with no commit/time/dirty fields available. → **Mitigation**: accepted for now; not solved by this change. If it becomes a real problem, publishing pre-built binaries via GitHub Releases sidesteps it entirely, since most public users would grab a binary rather than build from an archive themselves.
- **[Risk]** The pre-tag pseudo-version format (`v0.0.0-<timestamp>-<hash>`) is verbose for everyday dev use. → **Mitigation**: accepted as-is; it's unambiguous and requires no additional formatting logic. Can be shortened later (e.g. to just a short commit hash) without any spec change, since the spec only requires *a* version string, not a specific format.

## Migration Plan

Not applicable - purely additive, no existing behavior changes, no data or config migration involved.

## Open Questions

- Exact wording/formatting of the no-args short line (e.g. `rest-o-matic v1.0.1-0...` vs. just the bare version string) and of the `--version` block's field labels - cosmetic choices, deferred to implementation, do not affect the spec's contract (which only requires the five pieces of information be present and identifiable).
