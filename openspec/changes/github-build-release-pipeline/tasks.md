## 1. Module Rename

- [x] 1.1 Change `go.mod`'s module path from `rest-o-matic` to `github.com/drewlsvern/rest-o-matic`
- [x] 1.2 Update every internal import (`rest-o-matic/internal/...`) across `cmd/rest-o-matic/*.go`, `internal/*/*.go`, and their `_test.go` files to the new `github.com/drewlsvern/rest-o-matic/internal/...` prefix
- [x] 1.3 Run `go build ./...`, `go vet ./...`, and `go test ./...` to confirm the rename introduced no breakage

## 2. CI Build Check Workflow

- [x] 2.1 Add `.github/workflows/ci.yml` triggered on `pull_request` targeting `main` (default event types: opened, synchronize, reopened)
- [x] 2.2 Job runs `go build ./...`, `go vet ./...`, and `go test ./...` against the PR's current head
- [x] 2.3 Confirm the workflow uses a plain/default (shallow) checkout — no `fetch-depth: 0` needed here, since this pipeline never reports or depends on version strings

## 3. Release Workflow: Trigger and Safety Checks

- [x] 3.1 Add `.github/workflows/release.yml` triggered on tag push matching `v*`
- [x] 3.2 Checkout step uses `fetch-depth: 0` (full history and tags)
- [x] 3.3 Add a step that validates the pushed tag against strict semver (`vMAJOR.MINOR.PATCH` with an optional hyphenated pre-release suffix), aborting the job immediately with a clear error on a mismatch, before any other release work runs
- [x] 3.4 Add a step that verifies the tagged commit is an ancestor of `origin/main` (`git merge-base --is-ancestor`), aborting the job immediately if it is not
- [x] 3.5 Unit-verify both checks by hand once the workflow exists: push a deliberately malformed tag and confirm it's rejected before any build step runs; push a tag on a commit not reachable from `main` and confirm it's rejected the same way

## 4. Release Workflow: Build, Archive, Checksum, Changelog

- [x] 4.1 Add `.goreleaser.yaml`: builds for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64` (explicitly excluding `windows/arm64`), with `CGO_ENABLED=0` and no `-ldflags` version injection
- [x] 4.2 Confirm default archive format is used (`.tar.gz` for linux/darwin, `.zip` for windows) and default checksum generation covers all archives
- [x] 4.3 Configure changelog `groups` to bucket commits by conventional-commit prefix (features, fixes, etc.), commit range computed from the previous tag
- [x] 4.4 Confirm GoReleaser's own pre-release detection (hyphenated tag suffix) is left at its default behavior, marking such releases as pre-releases automatically
- [x] 4.5 Wire the `goreleaser/goreleaser-action` step into `release.yml`, running only after the checks in Section 3 pass

## 5. Manual, External Setup (Not a Repo File)

- [x] 5.1 In the GitHub repository's branch protection settings for `main`, require the CI workflow's status check and an approving review before the merge button unlocks — this is a GitHub UI/API setting, not something committed to the repo; do not consider this change complete until it's actually configured

## 6. End-to-End Verification

- [x] 6.1 Open a throwaway PR against `main` and confirm the CI workflow runs, and that the PR cannot merge until it passes
- [x] 6.2 Push a real test tag (e.g. a pre-release like `v0.0.1-rc.1`) and confirm: the release runs, all five platform archives and a checksums file are attached, the changelog is grouped by commit type, and the release is marked as a pre-release
- [x] 6.3 Download one of the release binaries and confirm `--version` reports the exact tag
- [x] 6.4 Once the repo is public, confirm `go install github.com/drewlsvern/rest-o-matic/cmd/rest-o-matic@<tag>` resolves and the installed binary reports the correct version with commit/commit-time/dirty shown as "unknown" (expected, per design.md)

## 7. Documentation

- [x] 7.1 Update the README's installation section to mention `go install github.com/drewlsvern/rest-o-matic/cmd/rest-o-matic@latest` as a supported installation method, alongside the existing `go build` instructions
- [x] 7.2 Briefly document the release process for future reference: tagging is manual (`git tag vX.Y.Z && git push origin vX.Y.Z`), not automated
