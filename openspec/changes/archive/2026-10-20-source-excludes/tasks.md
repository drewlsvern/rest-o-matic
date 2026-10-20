## 1. Config

- [x] 1.1 `Source` gains `Exclude []string`, `ExcludeCaches bool` and `OneFileSystem bool`. Its unmarshaller accepts the three modifier keys alongside `paths`, still rejects any other key as an unsupported source type, and draws did-you-mean suggestions from both key sets
- [x] 1.2 `Load` runs `expandHome` over each exclude pattern, with the job and pattern in the error when the home directory is unknown
- [x] 1.3 `config.schema.json`: the three keys under `source`, with descriptions and types (`exclude` an array of strings, the flags booleans)
- [x] 1.4 Tests: exclusions accepted with paths; exclusions without paths rejected as missing paths; `container` still rejected; `exlude` suggests `exclude`; `exclude: Trash` and `exclude_caches: "yes"` are type errors located at their line; `~/x` expanded, `~$*` and `~alice/x` left alone; unknown home directory; the schema reflection test passes and the example config is still valid

## 2. Validation

- [x] 2.1 Pattern syntax check (each component except `**` through `filepath.Match`) and empty-pattern check, as errors with path `backups.<job>.source.exclude.<i>`
- [x] 2.2 Anchored-pattern comparison against the job's source paths, component by component: a warning when a pattern covers a source path (naming the paths), a warning when it is outside all of them, and no warning once `**` is reached
- [x] 2.3 Tests: `/data/[abc` error; `""` error; `/home/me/.cache` outside `/home/me/documents` warns and stays valid; `/home/me` over `.ssh` and `.config` warns naming both; `/home/*/.cache` under `/home/me` and `/home/**/x` give no warning; `Trash` not checked; `validate --json` locates each at its list item

## 3. Execution

- [x] 3.1 `ResticRunner.Backup` takes the job's `Source` and adds `--exclude <p>` per pattern in order, then `--exclude-caches` / `--one-file-system` when set, after the paths and before `--tag`. Update the callers
- [x] 3.2 Unit tests on the built arguments: patterns in order, flags only when set, a job without exclusions gives exactly the old arguments, both repositories get the same exclusions, and a `podman-unshare` job's wrapped command carries them
- [x] 3.3 Integration test against real restic: a source with an excluded absolute directory, a bare-name directory at two depths, and a valid `CACHEDIR.TAG` directory with `exclude_caches: true`; the snapshot contains only what should remain. Then add a pattern and run again; the second snapshot records the first as its parent
- [x] 3.4 Run the integration test with restic 0.16 as well as the current version

## 4. Windows

- [x] 4.1 On the Windows CI job, back up a temporary directory with an absolute exclude written with backslashes (the form `~` expansion produces) and with forward slashes, and record which restic honours. Result (restic 0.19.1 on windows-latest, PR #31): both forms are honoured
- [x] 4.2 If backslash patterns are not honoured, convert separators in expanded patterns on Windows in `Load`, and keep the test as a regression check. Either way, note the result in `docs/configuration.md`. Not needed: backslash patterns work, so no conversion was added; the docs say both forms work

## 5. Docs and Verification

- [x] 5.1 `docs/configuration.md`: a "Leaving things out" section covering the three keys, restic's pattern rules (anchored, bare names match anywhere, `**`), `~` expansion including `~$*`, what each flag skips, the validation warnings, and that excludes don't break parent-snapshot reuse
- [x] 5.2 `rest-o-matic.example.yaml`: an `exclude` example in the commented `source` section
- [x] 5.3 UI repo handoff: the new `source` keys arrive through `rest-o-matic schema`, and the check-in contract is unchanged
- [x] 5.4 `go test ./...` and `go vet ./...` pass locally, and CI (including the Windows job) is green
