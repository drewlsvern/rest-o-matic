## 1. Secrets Package

- [x] 1.1 Add `filippo.io/age` to `go.mod`; confirm `CGO_ENABLED=0` builds for linux, darwin and windows still pass
- [x] 1.2 `internal/secrets`: resolve the default key path (`<user config dir>/rest-o-matic/host.key`) and the recovery recipients path beside whichever key path is in use
- [x] 1.3 `Generate(keyPath)`: create the directory `0700` and the key `0600` in age's identity file format, refusing if the file exists; return the public key
- [x] 1.4 `LoadIdentity(keyPath)`: read the key, returning a distinct "no host key" error when missing and, on Linux and macOS, an error when the file is readable by group or others; Windows build skips the mode check
- [x] 1.5 `LoadRecipients(path)`: read an age recipients file; a missing file is no recipients
- [x] 1.6 `Lock(plaintext, recipients)` returning single-line base64, and `Unlock(locked, identity)` returning distinct errors for "malformed" and "not locked for this key"; `WellFormed(locked)` for validation
- [x] 1.7 Unit tests: round trip; two recipients each unlock; wrong key gives the "not for this key" error; malformed input; locking twice differs; existing key not overwritten; `0644` key refused; missing recipients file is empty

## 2. Config

- [x] 2.1 Add `config.Secret` (text plus locked flag) with `UnmarshalYAML` recognising the `!locked` tag; change `Repository.Password` and `Repository.Env` to use it
- [x] 2.2 In `Load`, walk the YAML node tree, following aliases, and reject a `!locked` node anywhere except `repositories.<name>.password` and `repositories.<name>.env.<NAME>`, with its line number
- [x] 2.3 `Validate`: a locked value that is not well-formed is an error naming the repository and field; no key is read
- [x] 2.4 Tests: plain config unchanged; mixed locked and plain accepted; locked `url` rejected with line; locked value in a hook rejected; malformed locked value fails validation; a locked value reused through an anchor is accepted; validation passes with no key present

## 3. Unlocking at Run Time

- [x] 3.1 Give `ResticRunner` an unlocker that loads the host key at most once; make `repoEnv` return an error and resolve locked `password` and `env` values through it
- [x] 3.2 Update `Backup`, `Forget` and `PassThrough` for the error, wording it with the repository, the field and the reason, and never including the locked text
- [x] 3.3 Add the global `--key-file` flag and pass the resolved path to the runner in `run`, `tick` and `exec`
- [x] 3.4 Tests: locked password and locked env value reach restic as plain text; no host key and wrong key each fail that repository without starting restic; a job's other repository is still attempted; `exec` on one repository succeeds while another is locked for a different key
- [x] 3.5 Test with real restic: back up to a repository whose password is locked, then `exec <repo> -- snapshots` lists the snapshot

## 4. Secret Commands

- [x] 4.1 `secret keygen`: create the key, print its public key and path, and print a notice when no recovery recipients file exists
- [x] 4.2 `secret public-key`: print only the public key; fail when there is no host key
- [x] 4.3 `secret lock`: read from stdin when it is not a terminal (dropping one trailing newline), otherwise prompt twice without echo; refuse an empty value; encrypt to the host key, the recovery recipients and each `--recipient`; fail with a pointer to `secret keygen` when there is no recipient at all; print `!locked "..."`
- [x] 4.4 `secret reveal <repository> [NAME]`: print the plain text only; fail for an unknown repository or name, a value that is not locked, or one that can't be unlocked
- [x] 4.5 `secret check`: try every locked value, list each failure with repository, field and reason, exit non-zero if any failed, and pass a config with none
- [x] 4.6 Command tests: keygen then lock then reveal round trip through a config; `--recipient` for a second key; lock with no key fails; reveal of a plain value fails; check reports the one value locked for another key
- [x] 4.7 Test that a value produced by `secret lock` is opened by the stock `age` tool when it is installed (skipped otherwise); `age` is not installed here, so the automated test skips, and the check was done by hand with an `age` v1.3.2 built from source, in both directions

## 5. Docs and Verification

- [x] 5.1 README "Secrets" section: creating a recovery key first, `keygen`, locking a value, the recovery recipients file, `reveal` and `check`, opening a value with `age`, and what locking does and does not protect against
- [x] 5.2 `rest-o-matic.example.yaml`: locked forms of `password` and an `env` value alongside the plain ones
- [x] 5.3 Manual check in a rootless Podman user namespace: a key created by one user is not usable by another, and a `0644` key is refused
- [x] 5.4 Manual check: lock a value on one "host" for a second key with `--recipient`, and confirm the second can run a backup and the first's `secret check` passes
- [x] 5.5 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` pass; `openspec validate locked-secrets --strict` passes
