## 1. `!plain`

- [x] 1.1 `config.Secret` records a `!plain` marker; values marked so behave as plain text everywhere
- [x] 1.2 The placement check accepts `!plain` only on `repositories.<name>.env.<NAME>`, and rejects it elsewhere (including `password`) with its line
- [x] 1.3 Tests: a marked value reaches restic unchanged; `!plain` on a password, a url and a hook is rejected; a marked value shared through an anchor is accepted where used

## 2. `secret relock`

- [x] 2.1 `internal/config`: list every `!locked` scalar in a config file's node tree with its repository and field (following anchors, each node once)
- [x] 2.2 `secret relock`: open every value with the host key or `--with-key`; lock each for the host key and the recovery recipients; replace each old text with the new one in the file's bytes; parse the result again and open every value with the host key before writing; write atomically with the original mode
- [x] 2.3 `--dry-run`, the all-or-nothing failure listing each value that can't be opened, the no-host-key error, and a notice when there is no recovery key
- [x] 2.4 Tests: values locked for the host only afterwards open with a newly added recovery key; replacing a host with `--with-key`; a file with comments, blank lines, an anchor and inline maps is unchanged apart from the locked values; one unopenable value leaves the file untouched; dry run writes nothing; no host key; the file's mode is kept; the plain text never appears in output

## 3. Docs and Verification

- [x] 3.1 `docs/secrets.md`: re-locking, with the three situations that need it (a recovery key added later, replacing a lost host, replacing a key); restoring a lost host's backups with the recovery key; `!plain`
- [x] 3.2 `docs/cli.md` and the README command table
- [x] 3.3 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` pass; `openspec validate secret-relock --type change --strict` passes
