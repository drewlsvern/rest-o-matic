## Why

To see what snapshots a job has, you have to run `rest-o-matic exec <repo> -- snapshots --tag <job>` against each repository, which talks to the backend, needs its credentials, and can't be done for a repository that is unreachable right now. There is no way to see every job's snapshots in one place.

This is also the first part of the check-in work in [docs/design/central-management.md](../../../../docs/design/central-management.md). The central app will show snapshot lists from what each host reports, so the host has to hold them locally first.

## What Changes

- After a job's retention step succeeds for a repository, rest-o-matic records the snapshots that job now has there: each snapshot's ID, time, host, paths and tags, and, where restic reports them, its size and file count and what it changed (files that were new, files that had changed, and how much it added to the repository).
- The list comes from the retention step itself. `restic forget --json` already returns the snapshots it kept, so no extra restic command runs and no extra call is made to the repository.
- Lists are kept per job and repository in the state directory, with the time each was taken.
- `rest-o-matic status <job>` shows the job's snapshots in each repository, with an "as of" time. `status --json` reports, for every job and repository, when the list was taken, how many snapshots it has and the newest one; for a single job it includes the snapshots themselves.
- A list is only replaced when the retention step succeeds. If a backup or retention fails, the previous list stays, with its original time.
- Changes made to a repository outside rest-o-matic show up the next time the job runs.

## Capabilities

### New Capabilities
- `snapshot-lists`: what is recorded about each job's snapshots, when it is refreshed, and where it comes from.

### Modified Capabilities
- `job-status`: `status` shows the recorded snapshot lists.

## Impact

- `internal/execution`: `Forget` runs with `--json` and returns the kept snapshots.
- `internal/state`: a snapshot list file per job under `<state-dir>/snapshots/`.
- `cmd/rest-o-matic`: `run` and `tick` save the lists; `status` reads them.
- README and `docs/status.md`.
- No config change. The state file itself is unchanged, so it stays small.
- Works with restic 0.16, which returns the kept snapshots but not their sizes or what they changed; those show as unknown there.
