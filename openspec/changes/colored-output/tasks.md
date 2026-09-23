## 1. Colour Package

- [x] 1.1 Add `golang.org/x/term` to `go.mod`
- [x] 1.2 Create `internal/color` with `Painter` (`Error`, `Warn`, `Success`; red `31`, orange `38;5;208`, green `32`), returning the input unchanged when off, plus package-level `Stdout` and `Stderr` painters defaulting to off
- [x] 1.3 Implement `Decide(mode, noColor, isTTY)` following the precedence in the spec (flag `always`/`never` > non-empty `NO_COLOR` > terminal check), returning an error for any mode other than `auto`/`always`/`never`
- [x] 1.4 Unit tests: each `Painter` method's exact output when on, identity when off, and a table test of `Decide` covering every precedence case and an invalid mode

## 2. Root Command Wiring

- [x] 2.1 Add the persistent `--color` flag (default `auto`) to the root command
- [x] 2.2 Add `PersistentPreRunE` that validates the flag, sets `color.Stdout`/`color.Stderr` using `x/term` on each stream's file descriptor, and sets cobra's error prefix to `color.Stderr.Error("Error:")`

## 3. Call Sites

- [x] 3.1 `validate.go` and `root.go`: colour `config error:` (red), `config warning:` (orange) on stderr, and `config is valid` (green) on stdout
- [x] 3.2 `exec.go` `printResult`: colour `OK`/`FAILED`, and repository `ok`/`deferred`/`backup failed`/`forget failed`, keeping each colon and restic's error text outside the colour
- [x] 3.3 `tick.go`: colour `skipping job` (orange), and `<n> succeeded` (green) / `<n> failed` (red) in the summary only when n > 0
- [x] 3.4 `exec_cmd.go`: replace the `execMessagePrefix` constant with a severity-aware prefix; config error, undefined repository and gate refusal are red, config warning and lock-blocked are orange

## 4. Tests

- [x] 4.1 Confirm the existing CLI tests (piped streams, so colour is off under `auto`) pass unchanged, proving plain output is byte-identical
- [x] 4.2 CLI test: `validate --color=always` on a config with an error and a warning emits the red and orange sequences around exactly `config error:` and `config warning:`
- [x] 4.3 CLI test: `NO_COLOR=1` with `--color=always` still emits colour; `NO_COLOR=1` with the default emits none
- [x] 4.4 CLI test: `--color=sometimes` fails without running the command
- [x] 4.5 CLI test: `exec --color=always -- snapshots --json` stdout still matches direct restic output byte-for-byte
- [x] 4.6 CLI test: `run --color=always` colours `OK` and repository `ok` green

## 5. Docs and Verification

- [x] 5.1 README: short note on colour, the `--color` flag and `NO_COLOR`
- [x] 5.2 `go test ./...` and `go vet ./...` pass
- [x] 5.3 Manually run `validate` and `run` in a real terminal to eyeball the colours, including orange
- [x] 5.4 `openspec validate colored-output --strict` passes
