# Running restic commands

For anything `backup`/`forget` don't cover — `snapshots`, `check`, `restore`,
`mount`, `diff`, `dump`, and so on — `exec` injects a repository's connection
info and passes the rest straight to the real `restic` binary:

```sh
rest-o-matic exec nas -- snapshots
rest-o-matic exec nas -- restore a1b2c3d4 --target /tmp/restore
```

Output and the exit code are restic's own, completely unmodified — `exec` is
a transparent passthrough, not a parsed/reinterpreted wrapper like
`backup`/`forget`.

## Initialising a repository

rest-o-matic never creates a repository. Before a repository's first backup,
initialise it once:

```sh
rest-o-matic exec nas -- init
rest-o-matic exec nas -- snapshots      # lists nothing yet
```

Until you do, backups to it fail with "repository does not exist". Keep the
repository's password somewhere other than the machine being backed up:
without it the backups can't be read.

## Listing and restoring

```sh
rest-o-matic exec nas -- snapshots
rest-o-matic exec nas -- snapshots --tag documents      # one job's snapshots
rest-o-matic exec nas -- restore a1b2c3d4 --target /tmp/restore
```

restic recreates each file's full original path beneath `--target`, so
`/home/me/documents/a.txt` comes back as
`/tmp/restore/home/me/documents/a.txt`. Restore into an empty directory and
move what you need into place.

Every snapshot carries its job's name as a tag, which is how one repository
can hold several jobs. Data written by rootless Podman containers has to be
restored through its job; see
[Backing up container data](containers.md).

## Safeguards

Two safeguards apply, and they're independent of each other:


- **Locking** mirrors restic's own shared/exclusive lock model rather than
  an invented "dangerous command" list: read-oriented subcommands
  (`snapshots`, `ls`, `find`, `diff`, `stats`, `cat`, `check`, `mount`,
  `restore`) run immediately; everything else — including any subcommand
  rest-o-matic doesn't recognize — needs the same per-repository lock
  `backup`/`forget` use, and is refused at once (not run, exit code `20`) if
  another execution currently holds it. Unlike a job, `exec` never waits.
  `--force` skips this check only; restic's own internal locking is still
  the real backstop either way.
- **A tag-safety gate**, with no override flag at all: `forget` and `tag`
  are refused outright — and `restore latest` refused when no explicit
  snapshot ID is given — against a repository more than one job references,
  unless your own command already includes `--tag`. This is what stops an
  unscoped `forget --prune` from quietly pruning a different job's snapshots
  out of a repository they share. `--force` does **not** bypass this — the
  only ways around it are adding `--tag <job-name>` yourself, or running
  `restic` directly outside of `exec`.

## Exit codes

When `exec` refuses to invoke restic at all, it uses one of four reserved
exit codes instead of restic's own: `20` means it was blocked by the lock
(retry later, or use `--force`), `21` means it was blocked by the
tag-safety gate (the command itself needs `--tag`, not a retry), and `22`
means the state directory belongs to another user (see
[One user per state directory](concurrency.md#one-user-per-state-directory)), and `23`
means a restore through `--job` would use a snapshot taken in a different
read mode (see [Backing up container data](containers.md)). Whenever
restic is actually invoked, its own exit code is returned unchanged instead.

A lock-blocked message says the repository is "in use by another
execution". It doesn't yet say which job or process holds it.
