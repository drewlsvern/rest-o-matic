# Development

## Building

```sh
go build -o rest-o-matic ./cmd/rest-o-matic
```

The release builds are plain Go with cgo disabled, for Linux, macOS and
Windows.

## Tests

```sh
go test ./...
go vet ./...
```

Some tests need tools on your `PATH` and skip without them:

| Tool | Used by |
|---|---|
| `restic` | Integration tests that take real backups |
| `podman`, able to run `podman unshare` | The `read_as: podman-unshare` tests |
| `age` | One test that opens a locked value with the stock tool |

CI installs restic and Podman, and fails if `podman unshare` can't run, so
those tests are never silently skipped there. The Unix-only tests are
excluded on Windows by build tags.

The integration tests create throwaway repositories in temporary
directories. restic keeps a cache directory for each one under
`~/.cache/restic`; set `RESTIC_CACHE_DIR` to put them elsewhere.

## Version

Running `rest-o-matic` with no arguments shows a short version line above the
usual command listing; `rest-o-matic --version` (or `-v`) shows a fuller
build-info block — commit, commit time, whether the working tree was dirty at
build time, and the Go version used to build it.

The version is derived entirely from Go's own automatic build-info stamping
(no hand-maintained version number, no build script required) — `go build`
alone is enough for it to work correctly, both for an untagged dev build and,
later, for a build made at a tagged release commit. The one thing it depends
on is building from an actual `.git` checkout — building from a source
archive with no `.git` directory (e.g. GitHub's "Download ZIP") won't have
commit/time/dirty detail available.

## Releasing

Every pull request into `main` runs a build/test check automatically
(`.github/workflows/ci.yml`); merging is blocked until it passes.

Cutting a release is a manual, deliberate action — nothing tags or publishes
a release automatically just because something merged. To release:

```sh
git tag v1.2.3
git push origin v1.2.3
```

Pushing a tag matching `vMAJOR.MINOR.PATCH` (optionally with a `-prerelease`
suffix, e.g. `v1.2.3-rc.1`) triggers `.github/workflows/release.yml`, which
validates the tag format, verifies the tagged commit is actually part of
`main`'s history, then uses [GoReleaser](https://goreleaser.com/) to
cross-compile binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64`, and `windows/amd64`, publish them with checksums and a
changelog (grouped by conventional-commit type) to a GitHub Release, and
mark a hyphenated pre-release tag as a pre-release automatically.

## Specs and changes

Behaviour is specified in `openspec/specs/`, one folder per capability.
Changes are proposed, designed and tracked under `openspec/changes/` and
moved to `openspec/changes/archive/` once merged. Larger design notes are in
[`docs/design/`](design/).
