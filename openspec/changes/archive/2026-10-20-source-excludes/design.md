## Context

- `Source` holds only `Paths`. Its unmarshaller ([types.go](../../../internal/config/types.go)) rejects every other key as an "unsupported source type", with a did-you-mean suggestion. This rule is how `container`, `quadlet` and friends are kept out until they exist.
- `Load` runs `expandHome` over each source path ([load.go](../../../internal/config/load.go)). `Validate` checks `source.paths` is non-empty ([validate.go](../../../internal/config/validate.go)).
- `ResticRunner.Backup(ctx, repo, mode, paths, tags)` builds `-r <url> backup <paths…> --tag … --json` ([restic.go](../../../internal/execution/restic.go)). The read mode wraps the whole command, so anything in `args` reaches restic in both modes.
- `config.schema.json` is embedded, and a test checks its keys against the config types by reflection, in both directions.
- What restic 0.19.1 does, checked against a scratch repository:
  - A `~` in a pattern is not expanded.
  - A matching directory is not entered.
  - When excludes change, the parent snapshot is still found.
  - The snapshot JSON records `excludes`.
  - An invalid pattern (`/x/[abc`) is rejected with `Fatal: --exclude: invalid pattern(s)`, but only when `backup` starts, which is after the before hooks.
  - An empty pattern is accepted.

  CI runs restic 0.16 and a Windows job.

## Goals / Non-Goals

**Goals:**
- A job can leave parts of its source out, using restic's own pattern language, so restic's documentation applies as written.
- Anything that would make restic refuse to start is caught by `validate` instead, before the hooks have stopped a service.
- A pattern that obviously does nothing, or that removes a whole source path, is pointed out.

**Non-Goals:**
- Checking patterns against the filesystem. `validate` reads nothing from the host (config-validation spec), so it can't tell whether a pattern matches a real file.
- Reporting what an exclusion actually removed from a snapshot.

## Decisions

### Config shape

```yaml
source:
  paths: [~/.ssh, ~/.config, ~/.local]
  exclude:
    - ~/.local/share/containers
    - ~/.local/share/Trash
  exclude_caches: false
  one_file_system: false
```

The new keys sit under `source`, not on the job, because they describe what is read from the source. When container sources arrive, a different type may have its own idea of what excluding means. There are no global or policy-level defaults: anchors under an `x-` key already let jobs share a list, and a hidden default would make it harder to see from a job what it leaves out.

The two flags are plain booleans, not `exclude_caches: <marker>`-style values. They map one-to-one to restic flags that take no value.

### Types and modifiers in `Source`

The unmarshaller gets two key sets: **types** (`paths` today) and **modifiers** (`exclude`, `exclude_caches`, `one_file_system`). A key in neither set is still an "unsupported source type" error, with the suggestion now drawn from both sets, so `exlude` suggests `exclude`. Modifiers are decoded with ordinary yaml.v3 typing, so `exclude: Trash` gives yaml.v3's type error at that line, the way other mistyped values are reported today.

Keeping the rejection (rather than turning unknown source keys into warnings like other unknown keys) is deliberate. `source` is where a future type would be declared, and silently backing up nothing because `container:` was ignored would be worse than an error.

### Home directory expansion

`Load` runs the existing `expandHome` over each exclude pattern, right after the paths. Its rule (`~` alone, or `~` plus a separator) leaves bare-name patterns that start with `~`, like `~$*`, alone. On Windows `filepath.Join` gives backslashes. Whether restic matches those is part of the Windows check below.

### Pattern checks in `validate`

restic's pattern language matches a pattern against a path one component at a time, using Go's `filepath.Match` rules for each component and treating `**` as any number of components. The checks reuse that:

- **Syntax**: each component of the pattern, except `**`, is run through `filepath.Match(component, "")`. `ErrBadPattern` makes it an error. This catches what restic would refuse at startup.
- **Empty pattern**: error. restic accepts it, but it can only be a mistake.
- **Anchored patterns** (starting from the root, after expansion) are compared with each source path, component by component, as far as the shorter of the two goes:
  - If the pattern's components all match the start of the source path, the pattern covers the whole source path. **Warning.**
  - If the source path's components all match the start of the pattern, the pattern is somewhere under it. Fine.
  - If neither holds for any source path, nothing it names can be backed up by this job. **Warning.**
  - Running into `**` before the comparison is settled counts as "may match", so no warning is given.
- Unanchored patterns are never compared, because they match at any depth by design.

Warnings rather than errors: a pattern outside the sources may be kept on purpose while paths are being rearranged, and errors on these would block a config that works.

Alternative considered: running `restic backup --dry-run` with the patterns. It would need a repository and the source files, which `validate` must not touch.

### Passing them to restic

`Backup` takes the job's `Source` instead of just its paths, and adds `--exclude <p>` for each pattern in order, then `--exclude-caches` and `--one-file-system` when set. These go after the paths and before `--tag`. One `--exclude` per pattern, rather than writing an `--exclude-file`, keeps them visible in a process listing and needs no temporary file, which a wrapped read mode would have to be able to read. `forget` is unchanged.

### `exclude_larger_than` (deferred)

This was decided during exploration and recorded here so it isn't revisited from scratch. It is left out because restic skips oversized files without any message, and the limit is not recorded in the snapshot. If it is added, it takes a size (`exclude_larger_than: 15G`), leaving it out means off, there is no boolean shorthand, and `validate` checks the size. restic accepts only `k`, `m`, `g` or `t` suffixes, and rejects `15GB` only at run time.

## Risks / Trade-offs

- [A bare-name pattern excludes more than intended: `Cache` removes every `Cache` directory] → Documented with an example. Not warned about, because it is often exactly what's wanted (`node_modules`).
- [Exclusions are silent: a snapshot doesn't say in rest-o-matic's output what it left out] → `validate` warns about the clearly wrong cases. restic records the patterns in each snapshot, so showing them in `status` is left for later.
- [Windows: restic's matching against `C:\...` paths, and backslashes from `~` expansion, haven't been checked] → A task tests a real backup with excludes on the Windows CI job before the change is done. If backslash patterns fail, `Load` converts separators in expanded patterns there, and the spec scenario examples stay as they are.
- [restic 0.16 (CI) behaves differently from 0.19] → `--exclude`, `--exclude-caches` and `--one-file-system` all predate 0.16. The integration tests run against both versions.
- [`one_file_system` leaves out a mount the user wanted] → It's off by default, and the docs say what it skips.

## Migration Plan

No migration. Every new key is optional, and leaving them out produces the same restic command line as before. Rolling back to an older binary with a config that uses the new keys fails at load with "unsupported source type(s) [exclude]", which names the problem.
