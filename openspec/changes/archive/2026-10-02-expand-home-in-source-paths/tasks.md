## 1. Expansion

- [x] 1.1 `internal/config`: in `Load`, replace a leading `~` (alone, or followed by a path separator) in each source path with the user's home directory; fail with the job and path when the home directory is unknown
- [x] 1.2 Tests: `~/x` and `~` expand; `~alice/x`, `/srv/~x` and `/data` are untouched; an unknown home directory is a load error naming the job and path; a repository `url` starting with `~` is untouched
- [x] 1.3 Test with real restic: a job with `paths: ["~/documents"]` backs up the directory under `HOME`

## 2. Docs and Verification

- [x] 2.1 README quick start and `docs/configuration.md`: say that a leading `~` is the home directory of the user running rest-o-matic, that nothing else is expanded, and what that means under `sudo`
- [x] 2.2 `go test ./...`, `go vet ./...` and `GOOS=windows go build ./...` pass; `openspec validate expand-home-in-source-paths --strict` passes
