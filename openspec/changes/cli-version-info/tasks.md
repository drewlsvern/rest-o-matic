## 1. Build Info Retrieval

- [x] 1.1 Implement a function that reads `runtime/debug.ReadBuildInfo()` once and extracts: `Main.Version`, `vcs.revision`, `vcs.time`, `vcs.modified`, and `GoVersion`, handling the case where build info or any of those settings is unavailable (e.g. no `.git` at build time) without panicking
- [x] 1.2 Unit tests for the extraction logic covering: all fields present, and one or more VCS settings missing

## 2. Short Version Line (No-Args Invocation)

- [x] 2.1 Wire the extracted version string into the root command so it appears as a single line when `rest-o-matic` is invoked with no arguments, without altering the existing usage/available-commands/flags listing
- [x] 2.2 Integration test: running the built binary with no arguments includes the short version line and still lists the existing commands

## 3. Detailed Build Info via --version

- [x] 3.1 Build the multi-line block (version, commit, commit time, dirty flag, Go version) once at startup and assign it as `rootCmd.Version`
- [x] 3.2 Adjust the version template so `--version` prints the block as-is rather than cobra's default one-line phrasing
- [x] 3.3 Integration test: `--version` output contains all five pieces of information, each identifiable

## 4. Regression Check

- [x] 4.1 Confirm `validate`, `run`, `tick`, and `exec` behavior and output are unchanged by running their existing test suites

## 5. Documentation

- [x] 5.1 Add a short README note on `--version` and the no-args version line, including the accepted limitation that building from a source archive without `.git` won't have commit/time/dirty detail available
