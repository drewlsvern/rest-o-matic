## Context

- `config.Repository.Backend` is unmarshalled but never read. `url` goes verbatim to `restic -r` in backup, forget, and exec.
- `config.Validate` returns `[]error` and checks only jobs (policy, sources, repository references). It checks nothing about the repositories themselves.
- `run` and `tick` go through `loadAndValidate` in `cmd/rest-o-matic/root.go`. `validate` calls `Load` and `Validate` directly. `exec` calls only `config.Load` and never validates.
- There is currently no concept of a warning anywhere in config handling.

See proposal.md for motivation and specs/ for the exact error/warning cases.

## Goals / Non-Goals

**Goals:**
- A single repository check, shared by `validate`, `run`, `tick`, and `exec`, so all four commands agree on what's valid.
- Warnings that go through the same path as errors, so no command can print one and forget the other.

**Non-Goals:**
- Rewriting or normalising `url` (explicitly rejected; see Decisions).
- Checking anything after the scheme: hostnames, bucket names, credentials, reachability. Those remain restic's job.
- Detecting that a repository was *already* created in the wrong place (e.g. a local directory named after an S3 host). Validation prevents new occurrences; cleanup is manual.

## Decisions

### Validate, don't auto-prefix
Using `backend` to prepend the missing scheme was considered. It would have made the original broken config work unchanged. It was rejected because the URL restic receives would then differ from the one in the config, which is the same kind of invisible gap that caused this bug. It would also create two valid spellings for every remote URL. A hard error at `validate` time costs the user one edit.

### Errors only for contradictions; warnings for "probably wrong"
A `backend`/`url` scheme contradiction is never intentional, so failing costs nothing legitimate. Under cron or systemd, warnings go to a mail spool or journal nobody reads, so a warning would not have prevented this incident. Unknown backend values and relative local paths *can* be intentional (a restic backend newer than rest-o-matic; a deliberately relative path in a hand-run setup), so they only warn. This also keeps the config spec's "any restic backend accepted" promise: nothing is refused just for being unfamiliar.

### Scheme table as a small map in `internal/config`
A `map[string]string` from backend name to required prefix (`"s3": "s3:"`, …, `"local": ""`), plus `"rest-server"` mapped to the same `rest:` entry. "Has a known scheme" means the URL starts with any non-empty prefix in the table. Per repository:

```
backend empty                       → error "backend is required"
backend not in table                → warning, stop
url has known scheme S:
    backend local                   → error (local with remote url)
    S != table[backend]             → error (contradiction, name both)
url has no known scheme:
    backend local, !filepath.IsAbs  → warning (relative path)
    backend remote                  → error (missing "<scheme>:" prefix)
```

Alternative considered: parsing the URL with `net/url`. Rejected: restic URLs aren't RFC 3986 (`sftp:user@host:/path`, `b2:bucket:path`), and a prefix check is exactly the question being asked.

### Return type: a result struct instead of `[]error`
`Validate` changes to return `Result{Errors []error; Warnings []Warning}` (names indicative). An extra return value was considered and rejected: a struct lets callers range over each list without positional mistakes, and room for future warning types comes free. The existing `ValidationError` gains an optional `Repository` field alongside `Job`, so messages read `repository "s3": …`.

The per-repository check is also exposed on its own (e.g. `CheckRepository(name, repo) (errs []error, warns []Warning)`). `Validate` calls it for every repository, and `exec` calls it for just its target.

### Output
- Errors keep the current `config error: …` prefix.
- Warnings use `config warning: …`, on stderr, printed *before* any errors so the error count stays the last line. `validate` still prints `config is valid` when only warnings exist.
- `exec` uses its existing `rest-o-matic: ` message prefix for both, satisfying the restic-exec "distinguishable messages" requirement.

### Exec exit code on rejection
`exec` already exits `1` when the target repository is undefined. A backend/url error is the same kind of problem (the repository config is unusable), so it follows that precedent and exits `1`, not a new reserved code. Reserved codes 20 and 21 stay for runtime refusals (lock, gate), where a script genuinely benefits from telling them apart.

## Risks / Trade-offs

- **[Risk] Existing broken configs start failing outright**, including scheduled `tick` runs. → That's intended: they were silently not backing up remotely. The error names the repository and the missing prefix, so the fix is one line.
- **[Risk] Existing configs without `backend` break.** → All known configs set it; the example and README always have. Release notes flag this as breaking.
- **[Risk] restic adds a new backend scheme.** → It gets a warning, not an error, until it's added to the table. A URL using that new scheme with `backend: local` would not be caught as a contradiction until then. Acceptable.
- **[Trade-off] Warnings on every `tick`** for a relative local path will repeat in logs each run. → Acceptable; that repetition is the point, and an absolute path silences it.
- **[Risk] Exit code 1 on exec rejection overlaps restic's own "fatal error" code.** → Same as today's undefined-repository case. Scripts that care can tell the two apart by the `rest-o-matic: ` prefix on stderr.

## Migration Plan

1. Before upgrading, run the new binary's `rest-o-matic validate` against each deployed config and fix any errors (add the `s3:`, etc. prefix; add `backend` if missing).
2. For a config that was affected, a local repository directory named after the URL will exist under the working directory rest-o-matic ran in. Either `restic copy` its snapshots into the real remote repository or discard it, then delete the directory.
3. Rollback: the previous binary ignores `backend`, so reverting needs no config changes.
