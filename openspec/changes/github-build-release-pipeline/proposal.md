## Why

rest-o-matic has no CI or release automation yet: nothing stops a broken build from merging into `main`, and there's no way to produce distributable binaries other than building by hand. Now that the project has a real GitHub remote and the CLI already has correct version-reporting (from the `cli-version-info` change), it's time to add the automation that makes both of those useful.

## What Changes

- Add a GitHub Actions workflow that runs the build and test suite on every pull request targeting `main` (on open, and on every subsequent push to the PR branch), so branch protection can require it to pass before a merge is allowed.
- Add a second, independent GitHub Actions workflow that runs when a `vMAJOR.MINOR.PATCH` (optionally `-prerelease`) tag is pushed, using GoReleaser to cross-compile `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64` binaries, archive them, generate checksums and a conventional-commit-grouped changelog, and publish a GitHub Release (marked as a pre-release automatically for hyphenated tags).
- The release workflow validates the pushed tag against strict semver and verifies the tagged commit is an ancestor of `main` before doing any release work, failing fast on either check.
- The release workflow checks out with full git history (`fetch-depth: 0`), which both GoReleaser's changelog and the existing `debug.ReadBuildInfo()`-based version reporting depend on to resolve correctly against tags.
- **BREAKING**: rename the Go module from `rest-o-matic` to `github.com/drewlsvern/rest-o-matic` (and update every internal import path accordingly), which is what makes `go install github.com/drewlsvern/rest-o-matic/cmd/rest-o-matic@vX.Y.Z` resolve at all. This is an internal/build-tooling change only — it does not change any command's behavior, flags, or output.

**Explicitly out of scope:** publishing a container image of the CLI (parked, not decided against — just not part of this change); any automated/commit-driven version-bumping (tagging stays a deliberate, manual action); configuring GitHub's branch-protection rule itself, which is a one-time setting made in GitHub's UI/API, not a file this repo can commit.

## Capabilities

### New Capabilities
- `ci-build-check`: Running the build and test suite against every pull request targeting `main`, so branch protection has something to require before allowing a merge.
- `release-pipeline`: Producing a multi-platform, checksummed, changelogged GitHub Release whenever a valid semver tag is pushed, with safety checks against malformed tags and tags that aren't actually part of `main`'s history.

### Modified Capabilities
(none — no existing capability's runtime behavior changes; the module rename is a build/import-path detail, not a behavioral requirement change to any existing spec)

## Impact

- New files: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.goreleaser.yaml`.
- `go.mod`'s module path changes, and every internal import across `cmd/rest-o-matic/*.go`, `internal/*/*.go`, and their `_test.go` files changes to match — repo-wide but mechanical, no logic changes.
- No changes to the config schema, backup execution, scheduling, concurrency control, exec passthrough, or version-reporting runtime behavior.
- Requires a one-time, out-of-band manual step: configuring GitHub branch protection on `main` to require the `ci-build-check` workflow's status and an approving review before merging. Not implementable as a repo file; tracked as a task so it isn't silently skipped.
