## 1. Repository Check

- [x] 1.1 Add the backend → scheme-prefix table in `internal/config` (`s3`, `sftp`, `rest`, `swift`, `b2`, `azure`, `gs`, `rclone`, `local`), with `rest-server` resolving to the `rest:` entry
- [x] 1.2 Add a `Warning` type and a `Repository` field on `ValidationError` so both errors and warnings can be scoped to a repository in their message
- [x] 1.3 Implement the per-repository check following the decision tree in design.md: missing backend → error; unknown backend → warning; known scheme with local backend → error; scheme contradicting backend → error naming both; no scheme with remote backend → error naming the required prefix; no scheme with local backend and relative path → warning
- [x] 1.4 Unit tests for the per-repository check covering every scenario in `specs/config/spec.md` (s3 missing prefix, https without prefix, sftp-vs-s3 contradiction, local with remote url, rest-server alias, unknown backend, relative local path, missing backend, and valid local/s3 cases producing nothing)

## 2. Config Validation

- [x] 2.1 Change `Validate` to return a result carrying both errors and warnings, and have it run the repository check for every declared repository alongside the existing job checks
- [x] 2.2 Update existing `Validate` callers and tests for the new return type
- [x] 2.3 Unit test: a config with a backend/url contradiction and an undefined-policy reference reports both errors
- [x] 2.4 Update existing test fixtures (`rest-o-matic.test.yaml`, any test configs and in-code `config.Repository` literals) so they pass the new check, e.g. local test repositories use absolute paths

## 3. Commands

- [x] 3.1 `validate`: print `config warning: …` lines to stderr before any errors; still print `config is valid` when there are only warnings
- [x] 3.2 `loadAndValidate` (used by `run` and `tick`): print warnings to stderr and proceed; fail on errors as today
- [x] 3.3 `exec`: after the undefined-repository check, run the repository check on the target only; on error print it with the `rest-o-matic: ` prefix and exit 1 without invoking restic; print any warnings with the same prefix, then proceed. Do not run whole-config validation
- [x] 3.4 Command-level tests: `validate` with warnings only exits 0 and shows warnings; `validate` with a missing-prefix s3 repo exits non-zero; `exec` against a missing-prefix repo never invokes restic; `exec` against a valid repo succeeds despite an unrelated job error elsewhere in the config

## 4. Docs

- [x] 4.1 `rest-o-matic.example.yaml`: replace the "informational" comment and the "does no backend-specific validation" header with the accepted backend values, the prefix each requires, and a note that `url` is passed to restic unchanged
- [x] 4.2 README config section: briefly document that `backend` is required and must match the `url` scheme, and that a relative local path warns
- [x] 4.3 Note the breaking change (`backend` now required; mismatched URLs now rejected) for the next release's notes

## 5. Verification

- [x] 5.1 `go test ./...` passes
- [x] 5.2 Manually run `rest-o-matic validate` against a config reproducing the original incident (`backend: s3`, url without `s3:`) and confirm it fails with a message naming the repository and the `s3:` prefix
- [x] 5.3 `openspec validate validate-repository-backend --strict` passes
