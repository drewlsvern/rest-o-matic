## Context

- All of rest-o-matic's own output is written with `fmt.Print*` / `fmt.Fprint*` directly in `cmd/rest-o-matic` (`validate.go`, `root.go`, `exec.go`, `exec_cmd.go`, `tick.go`). There are about 20 call sites and no shared output helper.
- exec prefixes its own messages with the constant `execMessagePrefix = "rest-o-matic: "`.
- Cobra prints the top-level `Error: <err>` line itself (`SilenceErrors: false`).
- The root command has no `PersistentPreRun` yet, and there is no colour code or colour dependency anywhere.
- The CLI tests run the built binary with piped stdout/stderr, so under `auto` they already exercise the plain path.

See proposal.md for motivation and specs/cli-output/spec.md for exact behaviour.

## Goals / Non-Goals

**Goals:**
- One place that decides whether colour is on for each stream, and one set of helpers every call site uses.
- Zero change to output bytes when colour is off.

**Non-Goals:**
- Colouring restic's output, or rewriting any message text.
- Bold, dim, backgrounds, icons, or other styling.
- Detecting terminal colour depth or supporting a 16-colour fallback for orange (see Risks).
- Colour in hook output. Hook output isn't printed today.

## Decisions

### A small `internal/color` package, no third-party library
The whole feature is three escape sequences plus a terminal check. `fatih/color` and `lipgloss` would each add a dependency tree for that, against the project's thin-wrapper style.

```go
type Painter struct{ on bool }
func (p Painter) Error(s string) string   // red     \x1b[31m … \x1b[0m
func (p Painter) Warn(s string) string    // orange  \x1b[38;5;208m … \x1b[0m
func (p Painter) Success(s string) string // green   \x1b[32m … \x1b[0m
```

When `on` is false, each helper returns `s` unchanged. That's what makes the byte-for-byte guarantee hold: call sites keep their exact format strings and only wrap the label argument.

The package exposes two package-level painters, `Stdout` and `Stderr` (default off), set once per process. Call sites pick the painter that matches the stream they write to, e.g. `color.Stderr.Error("config error:")`.

### Enablement decided once, in the root command's `PersistentPreRunE`
A pure function holds the rules so they're unit-testable without a terminal:

```go
func Decide(mode string, noColor string, isTTY bool) (bool, error)
```

`PersistentPreRunE` validates `--color` (an invalid value returns an error before any command runs), then sets `Stdout`/`Stderr` from `Decide(mode, os.Getenv("NO_COLOR"), isTerminal(fd))`.

Cobra only runs `PersistentPreRunE` after flags parse, so a flag-parsing error prints a plain `Error:`. That's acceptable.

### Terminal detection with `golang.org/x/term`
`term.IsTerminal(int(f.Fd()))` is correct across Linux and macOS. The dependency is small, and its only transitive dependency (`golang.org/x/sys`) is already in `go.mod`.

The stdlib alternative, checking `os.ModeCharDevice` on `Stat()`, was rejected: `/dev/null` is a character device, so it would report output redirected to `/dev/null` as a terminal.

### Cobra's `Error:` prefix via `SetErrPrefix`
In `PersistentPreRunE`, after colour is decided: `rootCmd.SetErrPrefix(color.Stderr.Error("Error:"))`. Cobra 1.10 supports this, and it leaves cobra's own formatting otherwise untouched.

### exec prefix becomes severity-aware
`execMessagePrefix` becomes a helper that returns `rest-o-matic: ` wrapped in the given severity. The trailing space stays outside the colour. Each existing exec message picks its severity per the spec table. The gate refusal's second line reuses the error severity.

### Coloured spans
- Job line: only `OK` / `FAILED`.
- Repository lines: `ok`, `deferred`, `backup failed`, `forget failed`, with the colon after a label left uncoloured, e.g. `color.Stdout.Error("backup failed") + ": " + err`. That keeps restic's error text outside the colour, as the spec requires.
- Tick summary: the whole `<n> succeeded` / `<n> failed` phrase, and only when n > 0.
- The config labels `config error:` / `config warning:` include their colon, since those are the labels as written today.

## Risks / Trade-offs

- **[Risk] Orange needs 256-colour support.** A 16-colour terminal (e.g. the Linux console) may render `38;5;208` as a nearby colour or ignore it. → Accepted: nearly every modern terminal emulator supports 256 colours, and the user asked for orange over yellow. It's a one-line change to `\x1b[33m` if needed.
- **[Risk] A missed call site stays uncoloured.** → Harmless (it just prints plain). The tasks enumerate every call site found in the context survey.
- **[Trade-off] Global painters are package state.** → Set exactly once per process, before any command runs. Unit tests construct `Painter` values directly instead of touching the globals.
- **[Risk] Colour escapes leak into a log when someone passes `--color=always` to a scheduled run.** → That's the explicit meaning of `always`. `auto`, the default, never does this.

## Migration Plan

No config or behaviour migration. Scheduled runs keep today's exact output. To opt out interactively, use `--color=never` or `NO_COLOR=1`.
