## Context

- After each backup, `runRepo` calls `Forget`, which runs `restic forget --tag <job> --keep-... --prune` and discards its output ([restic.go](../../../internal/execution/restic.go)).
- With `--json`, the first line `restic forget` prints is a JSON array of groups, each with the snapshots it kept and removed. Checked against restic 0.19.1 and 0.16.4 (what CI installs). On 0.16 that line is followed by prune's plain-text progress, and snapshots carry no `summary`, so no sizes.
- The state file is rewritten on every tick and every run, under a lock ([state.go](../../../internal/state/state.go)).
- A job holds its own lock for the whole run, so only one process ever writes anything belonging to that job.
- `status` builds one report struct that both its text and JSON are rendered from ([status.go](../../../cmd/rest-o-matic/status.go)).

See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Every job's snapshots visible instantly, for all repositories, with no backend contact.
- No extra load on any repository.
- The shape the later check-in report will carry.

**Non-Goals:**
- A live view. `rest-o-matic exec <repo> -- snapshots` remains the way to ask the repository itself.
- Refreshing on a timer or on demand. A list changes only when its job runs. The earlier idea of a daily refresh is dropped: every run refreshes the list for free.
- Sending anything anywhere. That is the check-in change.
- Listing files inside a snapshot.

## Decisions

### Take the list from `forget --json`
`Forget` gains `--json` and reads the kept snapshots from its output. The alternative, a separate `restic snapshots --json --tag <job>` after each run, is one more round trip to every repository on every run for the same information.

The job's snapshots in a repository are the union of `keep` across all groups in the output, sorted newest first. restic groups by host and paths, so a job whose paths changed has more than one group.

The parser takes the first output line that is a JSON array, which is the same on both restic versions checked, and ignores everything else. If there is none, `Forget` reports that it has no list, and nothing is saved.

### One file per job, outside the state file
Lists go in `<state-dir>/snapshots/<job>.json`, holding each repository's list and the time it was taken:

```json
{
  "repositories": {
    "nas": {
      "listed_at": "2026-10-02T02:01:17Z",
      "snapshots": [
        {
          "id": "49bcad91e0f1...",
          "short_id": "49bcad91",
          "time": "2026-10-02T02:00:05Z",
          "hostname": "prd-podman-01",
          "paths": ["/home/me/documents"],
          "tags": ["documents"],
          "size": 1288490188,
          "files": 4212,
          "files_new": 12,
          "files_changed": 3,
          "data_added": 60000000,
          "data_added_packed": 47185920
        }
      ]
    }
  }
}
```

- **Not in `state.json`,** because that file is rewritten on every tick. Ten jobs with seventy snapshots each would be several hundred kilobytes rewritten every minute to record one timestamp.
- **One file per job** needs no new locking. The writer is the process running the job, which holds the job's lock, so a read-modify-write of that job's file can't race. It is written to a temporary file and renamed, like the state file.
- `size` is restic's `total_bytes_processed` and `files` its `total_files_processed`. `files_new`, `files_changed`, `data_added` and `data_added_packed` are restic's own figures for what the snapshot changed, under restic's names. All are omitted when restic doesn't report a summary.

### What a snapshot changed, at no extra cost
The same summary says how many files were new or changed and how much data the snapshot added, before and after compression. Recording it lets the list answer "did last night's backup pick up my change?" without listing the snapshot's contents, which would need the repository. The text view shows the compressed figure, since that is what the repository grew by, and falls back to the uncompressed one.

### Saved by the caller, after the run record
`RepoOutcome` carries the list. `executeAndRecord` saves the lists for the repositories that produced one, using the run's finish time as `listed_at`. A failure to save is a warning, like a failed state write.

Repositories that failed, were not reached, or produced no list are left alone, which is what keeps the previous list and its time.

### `status` shows a summary for all jobs and the list for one
Each job in the report gains `snapshot_lists`, one entry per repository in config order:

```json
"snapshot_lists": [
  {"repository": "nas", "listed_at": "2026-10-02T02:01:17Z", "count": 3, "newest": "2026-10-02T02:00:05Z"},
  {"repository": "offsite", "listed_at": null, "count": 0, "newest": null}
]
```

For a single job each entry also has `snapshots`. The all-jobs document leaves them out to stay small; a host with many jobs would otherwise print megabytes. This is an added key, so `format_version` stays 1.

The all-jobs table is unchanged. `status <job>` adds a section per repository after the run history:

```
Snapshots in nas, as of 2 hours ago: 3
ID        TIME              SIZE     CHANGED
49bcad91  2026-10-02 02:00  1.2 GiB  12 new, 3 changed, +45.0 MiB
0a3ca2ce  2026-10-01 02:00  1.2 GiB  0 new, 1 changed, +3.1 KiB
e974615e  2026-09-30 02:00  1.1 GiB  4212 new, 0 changed, +1.0 GiB

Snapshots in offsite: not listed yet (a list is taken each time the job runs)
```

### `status` gets a reader function, like the lock probe
`buildStatus` already takes a function for "is this job's lock held". It takes another for "this job's snapshot lists", so tests supply lists without touching the disk. A missing file is no lists.

## Risks / Trade-offs

- **[Trade-off] The list is only as fresh as the job's last successful run.** Snapshots removed by hand, or by another tool, stay listed until then. → The "as of" time is always shown, and `exec <repo> -- snapshots` gives the live answer.
- **[Risk] restic changes what `forget --json` prints.** → The parser needs only a JSON array of groups with `keep` lists; anything else on the output is ignored. If it finds nothing, the job still succeeds and the old list stays.
- **[Trade-off] No sizes or change figures on restic 0.16.** → Shown as unknown. Everything else works.
- **[Risk] Files for jobs that no longer exist are left behind.** → They are small and ignored. Not cleaned up here.
- **[Trade-off] A job name is used as a file name,** as it already is for the job's lock.

## Migration Plan

Nothing to do. Lists appear as jobs run. Until a job has run under this version, `status` shows its repositories as not listed yet.
