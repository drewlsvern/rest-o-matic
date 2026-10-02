## 1. Reading the List

- [x] 1.1 `internal/state`: add `Snapshot` and `SnapshotList` types, and `LoadSnapshots`/`SaveSnapshots` for `<state-dir>/snapshots/<job>.json` (atomic write; a missing file is no lists; saving one repository leaves the others untouched)
- [x] 1.2 `internal/execution`: run `forget` with `--json`, take the first output line that is a JSON array of groups, and return the union of their `keep` snapshots newest first, or "no list" when there is none
- [x] 1.3 Unit tests for the parser with real output from restic 0.19 and 0.16 (array followed by prune text), several groups, an empty array, and output with no array
- [x] 1.4 Unit tests for the store: round trip, a second repository added without disturbing the first, a missing file, concurrent readers

## 2. Recording

- [x] 2.1 Carry the list on `RepoOutcome`; in `executeAndRecord`, save the list of each repository that produced one, with the run's finish time, warning if it can't be saved
- [x] 2.2 Tests with real restic: three runs with `keep-last 2` leave two snapshots listed, including the newest; a second job in the same repository is not in the first job's list; restic is invoked exactly twice per repository
- [x] 2.3 Tests: a failed backup, a failed before hook, and unreadable forget output each leave the previous list and its time unchanged, and none changes the job's outcome; with two repositories and one failing, only the other's list is refreshed

## 3. Status

- [x] 3.1 Add `snapshot_lists` to each job in the report: one entry per configured repository with `listed_at`, `count` and `newest` (nulls when unlisted), plus `snapshots` when a single job is requested
- [x] 3.2 `status <job>`: a section per repository after the run history, newest first, with ID, time, human-readable size and the "as of" time, or "not listed yet"
- [x] 3.3 Tests with a fixed clock for the JSON (summary for all jobs, full list for one, nulls when unlisted, unknown size) and the text
- [x] 3.4 Command test: `status <job>` shows the list with the repository deleted afterwards, proving nothing contacts it

## 4. What Each Snapshot Changed

- [x] 4.0 Record restic's `files_new`, `files_changed`, `data_added` and `data_added_packed` on each snapshot where reported; add them to the single-job JSON (null when unknown) and a CHANGED column to `status <job>`; tests for both restic forms and for the text

## 5. Docs and Verification

- [x] 5.1 `docs/status.md`, the README's feature list and `docs/restic-commands.md`: what the lists are, when they refresh, and how to get a live answer instead
- [x] 5.2 Run the recording tests with restic 0.16.4 on `PATH` as well as the installed version
- [x] 5.3 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` pass; `openspec validate snapshot-lists --strict` passes
