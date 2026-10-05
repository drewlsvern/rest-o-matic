## Why

The central app's config editor has to check a config by the same rules the host will apply, and show each problem where it is. It does this by running the `rest-o-matic` binary of the version the host runs (see "Validating config with the rest-o-matic binary" in [the design](../../../../docs/design/central-management.md)). Today `validate` prints text meant for a person: a program can't tell an error from a warning without parsing it, and nothing says which line a problem is on.

The editor also needs a description of the config's structure, so that it can offer the right keys as you type and mark a misplaced one before anything is validated.

Writing these up turned up a real gap. A misspelt key is silently ignored almost everywhere: a config with `hook:` instead of `hooks:`, or `retension:`, passes `validate`, and the hook never runs. Both the schema and the new output need to say something about unknown keys, so this change fixes that too.

## What Changes

- **`rest-o-matic validate --json`** prints one JSON document: whether the config is valid, and every problem found, each with its severity, message, the job or repository it concerns, the dotted path of the field, and the line and column in the file. Problems that stop the file being read at all (a YAML syntax error, a locked value in the wrong place, a file that can't be read) are reported the same way, so the output is always one document.
- **Lines and columns in plain `validate` too**: every problem's text is prefixed with `line N:` where the line is known.
- **Unknown keys are warned about.** A key that rest-o-matic doesn't read, anywhere in `policies`, `repositories` or `backups`, or at the top level unless it starts with `x-`, is reported as a warning naming the key and its line, and suggesting the nearest known key when one is close. `validate`, `tick` and `run` all print it, and the editor marks it. A warning, not an error, so that a config which works today keeps working after an upgrade. The key is still ignored: auto-correcting a misspelt key to the one it resembles was considered and rejected, because it would act on a guess, for instance running hooks someone had switched off by renaming `hooks:` to `nohooks:`.
- **`rest-o-matic schema`** prints a JSON Schema of the config file, built into the binary, so each release carries the schema that matches its own rules. It has a description for every key, the allowed values where there is a fixed set, and rejects unknown keys.
- **A written contract** for the `validate --json` document, in `contract/validate/v1/`, like the check-in contract: a JSON Schema and examples, with tests that check real output against it.

## Capabilities

### New Capabilities
- `config-validation`: the `validate` command, its text and JSON output, problem locations and exit status.
- `config-schema`: the JSON Schema of the config file and the command that prints it.

### Modified Capabilities
- `config`: unknown keys are reported as warnings.

## Impact

- `internal/config`: problems carry a path; locations are resolved from the YAML node tree; a walk for unknown keys; the schema file, embedded.
- `cmd/rest-o-matic`: `validate --json`, `schema`.
- New `contract/validate/v1/`.
- Docs: `docs/configuration.md` (unknown keys, editor support), `docs/cli.md`, README command table.
- A config with an unknown key now prints a warning on every `validate`, `tick` and `run`. Nothing that ran before stops running.
