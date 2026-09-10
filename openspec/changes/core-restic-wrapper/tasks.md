## 1. Project Setup

- [x] 1.1 Initialize the Go module and repository layout (`cmd/`, `internal/config`, `internal/execution`, `internal/schedule`, `internal/lock`, `internal/state`)
- [x] 1.2 Wire up the CLI framework with a root command and placeholder `validate`, `run`, and `tick` subcommands

## 2. Config Schema and Validation

- [x] 2.1 Define the `Source` type with a custom YAML unmarshaler that accepts only a `paths` shape and rejects any other key (`config` spec: Source Declaration)
- [x] 2.2 Define the `Repository` type as a backend type plus arbitrary backend-specific connection fields, with no backend-specific validation (`config` spec: Repository Declaration)
- [x] 2.3 Define the `Policy` type (schedule + retention map) and the top-level named `policies` collection (`config` spec: Named Policy Definition)
- [x] 2.4 Define the `Job` type: source, hooks, policy reference, optional job-level retention override, list of repository entries (each with an optional per-repository retention override), and optional user tags
- [x] 2.5 Implement full config file loading (YAML) into these types
- [x] 2.6 Implement validation: reject a job referencing an undefined policy name; reject a job referencing an undefined repository name; reject an empty/missing `paths` list (`config` spec: Job Policy Reference and Resolution, Repository Reference Validation, Source Declaration)
- [x] 2.7 Unit tests covering every Scenario in `specs/config/spec.md`

## 3. Retention Resolution

- [x] 3.1 Implement a pure key-by-key deep-merge function over retention maps
- [x] 3.2 Implement the three-level resolver: policy retention → job-level override → per-repository override, in that order, producing an effective retention per (job, repository) pair
- [x] 3.3 Unit tests covering every Scenario in `specs/config/spec.md` related to override merging (job-level partial override, no-override passthrough, per-repository override, independent per-repository resolution)

## 4. Restic Invocation Layer

- [x] 4.1 Implement translation from a `Repository` config into the `-r <url>` argument and environment variables restic expects, uniformly across backend types
- [x] 4.2 Implement `restic backup` invocation: source paths, automatic job-name tag, additive user tags, JSON output parsing for success/failure and snapshot info
- [x] 4.3 Implement `restic forget` invocation: tag-scoped to the job's automatic tag, using the resolved effective retention's `--keep-*` flags for that (job, repository) pair
- [x] 4.4 Surface restic's own error output on failure rather than swallowing or reinterpreting it

## 5. Job Execution (Hooks, Backup, Retention)

- [x] 5.1 Implement before-hook execution, run once per job execution, prior to any repository backup (`backup-execution` spec: Job-Level Hook Execution)
- [x] 5.2 On before-hook failure, skip all repository backups but still run after-hooks, and mark the job outcome failed (`backup-execution` spec: Before-Hook Failure Aborts Backup)
- [x] 5.3 Implement independent per-repository backup fan-out: one repository's failure does not stop attempts against the others (`backup-execution` spec: Independent Per-Repository Backup)
- [x] 5.4 Implement after-hook execution, run once per job execution after all repository attempts (`backup-execution` spec: Job-Level Hook Execution)
- [x] 5.5 Implement post-backup retention enforcement per (job, repository) pair using the resolved effective retention (`backup-execution` spec: Tag-Scoped Retention Enforcement)
- [x] 5.6 Aggregate and return a single job outcome (success/partial-failure/failure) across hook and per-repository results
- [x] 5.7 Unit/integration tests covering every Scenario in `specs/backup-execution/spec.md`

## 6. Cross-Process Locking

- [x] 6.1 Implement a non-blocking per-repository `flock`-based lock, acquired before any restic operation against that repository and released immediately after
- [x] 6.2 Implement `max_concurrent` global slot locks: pre-create N slot lock files, acquire the first available via non-blocking `flock`, hold for the job's full execution
- [x] 6.3 Wire job execution to defer (skip this pass, leave to the next tick or a future retry) when either lock type is unavailable, rather than blocking or erroring
- [x] 6.4 Unit/integration tests covering every Scenario in `specs/concurrency-control/spec.md`, including simulating a held lock from a separate process to verify deferral and duplicate-run prevention

## 7. State Store and Due-Detection

- [x] 7.1 Define the JSON state file schema: per-job `last_run` timestamp and `last_outcome`
- [x] 7.2 Implement atomic state file writes (write-to-temp-file-then-rename) guarded by an in-process mutex for concurrent goroutine updates
- [x] 7.3 Implement UTC calendar-boundary truncation functions for `hourly`, `daily`, and `weekly` schedules
- [x] 7.4 Implement due-detection: a job is due when `boundary(now) != boundary(last_run)` read from the state file
- [x] 7.5 Unit tests covering every Scenario in `specs/scheduling/spec.md` related to due-detection, catch-up, and state persistence

## 8. Tick Command

- [x] 8.1 Implement `tick`: load config, compute the due set across all jobs using the state store
- [x] 8.2 Dispatch the due set through an in-process worker pool sized to `max_concurrent`, preserving FIFO order within this tick's due set, applying the per-repository and slot locks from Section 6
- [x] 8.3 Update the state store after each dispatched job completes
- [x] 8.4 Ensure `tick` blocks until all jobs it dispatched have finished, then exits (no persistent process) (`scheduling` spec: Tick as the Sole Scheduling Entry Point)
- [x] 8.5 Integration test: a full `tick` invocation against a temporary local-backend repository, exercising due-detection, execution, and state update end-to-end

## 9. Run Command (On-Demand Execution)

- [x] 9.1 Implement `run <job>`: execute the named job immediately regardless of due status, reusing the hook/backup/retention execution path from Section 5 and the locking behavior from Section 6 (`backup-execution` spec: On-Demand Job Execution)
- [x] 9.2 Update the state store identically to a tick-triggered execution
- [x] 9.3 Integration test covering manual invocation bypassing due-checking

## 10. Validate Command

- [x] 10.1 Implement `validate`: load and validate the config file, reporting every validation error from Section 2 without executing any hooks or restic operations
- [x] 10.2 Test that `validate` reports all applicable errors for a config with multiple simultaneous issues (e.g. an undefined policy reference and an undefined repository reference)

## 11. Documentation

- [x] 11.1 Write a README covering installation, the config schema with the `documents`/`postgres` style examples from proposal.md, and how to wire up an external periodic trigger (cron, launchd, etc.) to call `tick`
- [x] 11.2 Document that container/Podman/Docker support and systemd generation are explicitly out of scope for this release and planned for later
