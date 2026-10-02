## 1. Ownership Check

- [x] 1.1 Add the check (`//go:build !windows`): stat the state directory, `locks/`, each regular file in `locks/`, and `state.json` (skipping any that don't exist), compare each owner uid with `os.Geteuid()`, and return one error listing every mismatched path with its owner
- [x] 1.2 Resolve uids to user names with `os/user.LookupId`, falling back to the numeric uid; the error names the running user and owner, and suggests running as the owner or passing `--state-dir`
- [x] 1.3 Add the Windows no-op (`//go:build windows`); `GOOS=windows go build ./...` succeeds
- [x] 1.4 Unit tests with an injectable owner lookup: missing directory passes; all-matching passes; mismatched directory fails; matching directory with a mismatched lock file fails and lists that file; `state.json` mismatch fails; unrelated files are ignored

## 2. Wire Into Commands

- [x] 2.1 `run` and `tick`: call the check after config load/validation and before taking the concurrency slot or running anything; on failure print the error and exit non-zero with nothing created
- [x] 2.2 `exec`: call the check immediately before `lock.AcquireRepository`, only on the path that takes the lock; on failure print the message with exec's error prefix and exit with a new reserved `ExitStateDirBlocked` (22)
- [x] 2.3 Tests: `exec` with a shared-lock subcommand or `--force` does not run the check; a locking subcommand does, and returns 22 without invoking restic
- [x] 2.4 Update exec's `--help` text and the restic-exec reserved-exit-code documentation to include 22

## 3. Docs and Verification

- [x] 3.1 README: a short "state directory" note covering one user per state directory, an absolute `--state-dir` for scheduled runs, and what the error means and how to fix it (delete or `chown` the listed files)
- [x] 3.2 Manual check on Linux: `sudo ./rest-o-matic tick` against a user-owned state directory refuses and creates nothing; root-owned leftovers under `locks/` are listed when running as the user
- [x] 3.3 `go test ./...` and `go vet ./...` pass; `openspec validate state-dir-owner-guard --strict` passes
