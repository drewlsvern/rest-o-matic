## 1. Config

- [x] 1.1 Add `ReadAs` to the job type with YAML key `read_as`; an empty value resolves to `direct`
- [x] 1.2 Validation: an unknown value is an error naming the job and the allowed values; `podman-unshare` on non-Linux (`runtime.GOOS`) is an error saying it isn't needed on that platform; `podman-unshare` on Linux with no `podman` found by `exec.LookPath` is a warning
- [x] 1.3 Config tests for the omitted/direct/podman-unshare/unknown cases, the platform error (with an injectable GOOS) and the missing-podman warning (with an injectable lookup)

## 2. Running restic in a Read Mode

- [x] 2.1 Add one helper that builds the restic `*exec.Cmd` for a read mode: `direct` gives today's command; `podman-unshare` resolves restic to an absolute path and builds `podman unshare <restic> <args…>`, with a clear error when `podman` can't be found
- [x] 2.2 Unix cancel for wrapped modes: `Setpgid`, SIGINT to the process group, `WaitDelay` kill of the group; `direct` cancel unchanged; Windows build compiles, and the wrapped path is unreachable there
- [x] 2.3 `Backup` takes the read mode and uses the helper; `runJob` passes the job's read mode and adds `restomatic-read=<mode>` to the tags for non-direct modes; `Forget` unchanged
- [x] 2.4 The exit-3 hint: on `ExitCode() == 3` with `permission denied` in stderr, add the `podman-unshare` hint (direct, Linux, podman found) or the owner-or-root hint; no hint otherwise
- [x] 2.5 Unit tests with a fake restic/podman on `PATH`: argument vector for each mode, tags with and without the read-mode tag, each hint case, and no hint for other exit codes

## 3. exec --job

- [x] 3.1 Add `--job <name>` to `exec`: refuse before restic when the job doesn't exist or doesn't list the target repository, naming both; otherwise run `PassThrough` through the read-mode helper, with no process group
- [x] 3.2 Tests: unknown job refused; job not using the repository refused; direct job gives an identical invocation; podman-unshare job wraps; the tag-safety gate still refuses untagged `forget` on a shared repository with `--job`
- [x] 3.3 Update exec's `--help` text
- [x] 3.4 Read-mode check for wrapped `restore` via `--job`: find the snapshot argument (fail closed when ambiguous); for an explicit ID look up its tags with a direct `restic snapshots --json <id>`; for `latest` require every `--tag` filter to include `restomatic-read=<mode>`; refuse with a new reserved `ExitReadModeBlocked` (23) and a message naming the snapshot, the mode and the fix
- [x] 3.5 Tests: snapshot-argument parsing (flags with values, `--flag=value`, `id:subfolder`, ambiguous); matching ID allowed; untagged ID refused; `latest` with and without the tag in every filter; direct jobs, plain exec and non-restore subcommands unchecked; an integration case with real restic where an untagged snapshot is refused and a tagged one restores

## 4. Integration Test (Linux, real Podman)

- [x] 4.1 Add a test that's skipped unless `podman` and `restic` are available: create a source with a `0600` file owned by a subordinate uid (via `podman unshare chown`), show that a `direct` backup exits 3, show that a `podman-unshare` backup succeeds, then restore through the wrapped path and check the file's host owner and mode match
- [x] 4.2 In the same test, confirm that `podman unshare` passes restic's exit code and the `RESTIC_PASSWORD` environment variable through, and that cancelling a wrapped backup's context stops restic (no restic process left, and `restic list locks` is empty afterwards)
- [ ] 4.3 Make sure the Linux CI job runs it (install Podman in the workflow if the runner lacks it)

## 5. Docs and Verification

- [x] 5.1 README "Container volumes" section: bind mounts via `paths` with absolute paths; rootless Podman (keep-id versus default mapping, when `read_as: podman-unshare` is needed, what the exit-3 hint means); restoring with `exec <repo> --job <job> -- restore … --target <scratch dir>` then `podman unshare mv` into place (why not `--target /`), and the read-mode check on restores; `restic mount` caveat; rootful Docker and Podman (run as root, with group-restricted `setcap cap_dac_read_search` as an advanced option that must be reapplied after restic upgrades); rootless Docker and named volumes not supported yet; update "What's not here yet"
- [x] 5.2 `rest-o-matic.example.yaml`: a commented `read_as` example on a container job
- [x] 5.3 `go test ./...` and `go vet ./...` pass; `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` succeed; `openspec validate podman-unshare-read-mode --strict` passes
- [x] 5.4 Manual check on a rootless Podman host: a keep-id container and a default-mapping container (or a mixed-uid pod) both back up with `podman-unshare`, and one file restores in place with the right owner
