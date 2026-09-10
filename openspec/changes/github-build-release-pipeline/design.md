## Context

See proposal.md for motivation. This touches only build/release tooling — `.github/workflows/`, a new `.goreleaser.yaml`, and `go.mod` plus every file's import paths. No package under `internal/` or `cmd/rest-o-matic/`'s command logic changes behavior. The relevant specs are `specs/ci-build-check/spec.md` and `specs/release-pipeline/spec.md` in this change.

This builds directly on the `cli-version-info` capability (already implemented, archived): its version reporting depends on `debug.ReadBuildInfo()` seeing full git history and tags at build time, which is why checkout configuration here isn't a free choice.

## Goals / Non-Goals

**Goals:**
- A PR check that's always accurate for the PR's current head, with no staleness window.
- A release pipeline that can't publish from a malformed tag or a tag outside `main`'s history.
- `go install` support via a corrected module path.

**Non-Goals:**
- Any container/Docker image publishing (parked, not part of this change).
- Any automated/commit-driven version bumping.
- Configuring GitHub branch protection itself — that's a manual, external, one-time step (see Migration Plan), not a file this repo can commit.

## Decisions

### CI trigger: plain `on: pull_request`, not `pull_request_review`
```yaml
on:
  pull_request:
    branches: [main]
```
Default event types (`opened`, `synchronize`, `reopened`) cover "runs when a PR is opened" and "re-runs on every new commit" without extra configuration. The alternative considered — triggering on `pull_request_review: types: [submitted]`, filtered to `review.state == 'approved'` — was rejected because it only validates whichever commit existed at approval time; keeping that safe requires branch protection's "dismiss stale reviews on new commits" to be correctly enabled too, adding a second setting that has to stay correctly configured for the whole thing to be safe. Plain `pull_request` has no such dependency: it is definitionally always checking the current head.

### CI job: build + vet + test, mirroring existing local practice
```yaml
- run: go build ./...
- run: go vet ./...
- run: go test ./...
```
No new tooling introduced — this is exactly the sequence already used throughout local development on this project.

### Release trigger: tag push, filtered further inside the job
```yaml
on:
  push:
    tags: ['v*']
```
GitHub Actions' tag-glob matching (`v*`) is not full regex, so it can't by itself distinguish `v1.2.3` from `v1.2` or `version-1`. The workflow's first step re-validates the actual tag against a strict semver pattern (`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`) and fails the job immediately if it doesn't match, before checkout of anything else meaningful happens.

### Ancestor check: one git command, run before any build step
```sh
git merge-base --is-ancestor "$GITHUB_SHA" origin/main || exit 1
```
Requires the checkout to have fetched `main` (implied by `fetch-depth: 0`, see below). Run immediately after tag-format validation, before GoReleaser is invoked.

### Checkout: `fetch-depth: 0` on the release workflow
```yaml
- uses: actions/checkout@v4
  with:
    fetch-depth: 0
```
Two independent consumers need this: the ancestor check needs `main`'s history reachable locally, and both GoReleaser's changelog (commit range since the previous tag) and this project's own `debug.ReadBuildInfo()`-based version reporting need full tag history to resolve correctly. The CI workflow (Pipeline 1) does not need this — a shallow default clone is fine there, since it only builds and tests, it doesn't report or depend on version strings.

### Release build: GoReleaser, not a hand-rolled matrix
A `.goreleaser.yaml` covering:
- `builds`: `goos: [linux, darwin, windows]`, `goarch: [amd64, arm64]`, with an explicit exclusion removing `windows/arm64` (only `windows/amd64` is wanted per the five-target list), `env: [CGO_ENABLED=0]`, main path `./cmd/rest-o-matic`.
- No `ldflags` version injection — the binary already self-reports its version correctly via `debug.ReadBuildInfo()` as established by `cli-version-info`, provided the checkout has full history (above). GoReleaser's build step should not set `-X`-style version flags that would conflict with or duplicate that.
- `archives`: default per-OS format (`tar.gz` for linux/darwin, `zip` for windows) — GoReleaser's default behavior already matches this, no override needed.
- `checksum`: default `checksums.txt` covering all archives — GoReleaser's default behavior.
- `changelog`: `groups` configured to bucket commits by conventional-commit prefix (e.g. `feat:` → "Features", `fix:` → "Bug Fixes"), commit range computed automatically from the previous tag.
- `release`: GoReleaser's own semver-prerelease detection (a tag with a hyphenated suffix) automatically marks the created GitHub Release as a pre-release — no explicit configuration needed beyond letting such tags reach GoReleaser at all (which the tag-format validation step already allows for).

### Module rename mechanics
1. `go.mod`: `module rest-o-matic` → `module github.com/drewlsvern/rest-o-matic`.
2. Every import of `rest-o-matic/internal/...` across `cmd/rest-o-matic/*.go`, `internal/*/*.go`, and their `_test.go` files → `github.com/drewlsvern/rest-o-matic/internal/...`.
3. `go build ./...`, `go vet ./...`, `go test ./...` re-run afterward to confirm the rename didn't break anything — purely mechanical, but repo-wide, so this is its own task rather than a one-liner folded into something else.

Accepted, unchanged characteristic (not addressed by this change): a `go install`-fetched binary has no local `.git` in its build (it's a module-proxy-served snapshot), so `Main.Version` will still correctly equal the requested tag, but the existing `--version` block's commit/commit-time/dirty fields will show `"unknown"` — identical to the already-documented no-`.git`-source-archive case in the README. This is `cli-version-info`'s existing graceful-degradation behavior working exactly as designed, not a gap this change needs to close.

## Risks / Trade-offs

- **[Risk]** GoReleaser is a new external dependency in the release path (not a Go module dependency, a separate CLI tool the Action installs). → **Mitigation**: it's the de facto standard for this exact use case, actively maintained, and its GitHub Action (`goreleaser/goreleaser-action`) is well-established; the alternative (hand-rolled matrix + archiving + checksums + changelog) is strictly more custom code to maintain, not less risk.
- **[Risk]** The ancestor-of-main check and tag-format validation are both shell/git commands embedded in workflow YAML rather than tested code. → **Mitigation**: both are single, well-understood commands (`git merge-base --is-ancestor`, a regex match) with low surface area; correctness can be verified by pushing a deliberately malformed tag and a deliberately off-branch tag once the workflow exists, without needing a unit-test harness for CI YAML.
- **[Risk]** Renaming the module is a breaking change to the import path for anyone who might depend on this as a library. → **Mitigation**: this project has never been published as a library (only as a CLI binary), so there are no known external consumers of the Go import path today; doing the rename now, before the repo is public, minimizes any future disruption.

## Migration Plan

1. Land the module rename and confirm `go build`/`go vet`/`go test` all still pass.
2. Add `.github/workflows/ci.yml`, `.github/workflows/release.yml`, and `.goreleaser.yaml`.
3. **Manual, external step (not a repo file, tracked so it isn't skipped):** in the GitHub repository's Settings → Branches, add a protection rule for `main` requiring the CI workflow's status check to pass and requiring an approving review, before the merge button unlocks.
4. Verify the CI workflow end-to-end by opening a throwaway PR.
5. Verify the release workflow end-to-end by pushing a real tag (e.g. a `v0.0.1` or pre-release tag) and confirming the resulting GitHub Release, artifacts, and `go install ...@v0.0.1` all work as expected.

No rollback complexity beyond removing the workflow files and reverting the module rename if something is fundamentally wrong — nothing here is destructive to existing data or state.

## Open Questions

- Exact GoReleaser config field names/version pin (the tool's config schema evolves between major versions) — an implementation-time detail, doesn't change the spec's behavioral contract.
- Whether `windows/arm64` should be added later — explicitly out of the five-target list agreed on for this change; adding it later is a one-line config addition, not a redesign.
