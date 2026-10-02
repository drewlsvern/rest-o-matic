## 1. Message Extraction

- [x] 1.1 `internal/execution`: add a function that turns restic's standard error into plain messages: the `message` of an `exit_error` object, the message of an `error` object (or operation, path and error number for restic 0.16's form), plain lines as they are; drop a leading `Fatal: `, keep the first line of each message, remove duplicates, and after the first three report how many more there were
- [x] 1.2 Use it for the errors built by `Backup`, `Forget` and `SnapshotTags`; keep the exit status, and keep giving the unreadable-files hint from restic's raw output
- [x] 1.3 Unit tests with restic's real output for: a missing repository, a wrong password, an unreadable file in both the current and the 0.16 form, a missing source path (plain line plus JSON), plain non-JSON output, empty output, and twenty errors

## 2. Verification

- [x] 2.1 Test with real restic: a job against an uninitialised repository reports "repository does not exist" with no JSON, and the recorded run's error is the same plain text
- [x] 2.2 Docs: update the example output in `docs/status.md` and anywhere else that shows the old JSON text
- [x] 2.3 `go test ./...`, `go vet ./...` and `GOOS=windows go build ./...` pass; `openspec validate readable-restic-errors --strict` passes
