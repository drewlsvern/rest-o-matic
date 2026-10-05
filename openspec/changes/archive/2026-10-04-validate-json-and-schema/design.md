## Context

- `config.Load` parses the file into a YAML node tree, checks where `!locked` and `!plain` appear, then decodes into Go types ([load.go](../../../../internal/config/load.go)). `config.Validate` then returns errors and warnings that know their job or repository, but not their field or line ([validate.go](../../../../internal/config/validate.go)).
- Decoding uses yaml.v3 without `KnownFields`, so an unknown key is ignored. A few custom unmarshallers (`notify`, a job's `source`, `after` hooks) reject unknown keys themselves, as errors.
- The central app (Blazor Server, Monaco) runs the binary matching the host's version to validate, and wants to place each problem in the editor. Monaco's YAML support (monaco-yaml, built on yaml-language-server) takes a JSON Schema for completion and structural checks.
- The check-in change set the pattern for a contract with the UI repo: `contract/<name>/v1/`, JSON Schemas, examples, tests against real output.

## Goals / Non-Goals

**Goals:**
- One JSON document from `validate` that an editor can turn straight into markers.
- A schema good enough for completion and hover help, which can't silently drift from what the host reads.
- A misspelt key is reported wherever the config is used, rather than silently ignored.

**Non-Goals:**
- Reading the config from standard input. The central app writes a temporary file, which holds only locked secrets.
- Expressing cross-references in the schema (a job's policy must exist). That is what `validate` is for; the editor runs it as you type, debounced.

## Decisions

### The JSON document

```json
{
  "format_version": 1,
  "rest_o_matic_version": "v0.2.0",
  "valid": false,
  "problems": [
    {"severity": "error", "message": "references undefined policy \"hott\"",
     "job": "docs", "repository": null, "path": "backups.docs.policy", "line": 8, "column": 13},
    {"severity": "warning", "message": "\"hook\" is not a key rest-o-matic reads; did you mean \"hooks\"?",
     "job": "docs", "repository": null, "path": "backups.docs.hook", "line": 10, "column": 5}
  ]
}
```

One list with a severity, rather than separate `errors` and `warnings`, because that is what an editor's markers are. Problems are sorted by line, then column; unlocated ones come first. `valid` is true when there are no errors. Line and column are 1-based, as YAML parsers and Monaco both count. Columns count characters, not bytes.

The exit status is unchanged: 0 when valid, 1 otherwise, including when the file can't be read.

### Problems learn their path

`ValidationError` and `Warning` gain a `Path`: the dotted path of the field, built where each problem is found (`backups.<job>.policy`, `repositories.<name>.url`). That is a small change at each of the places that create one. The text form doesn't change apart from the `line N:` prefix.

### Locations come from the node tree

Once validation is done, each path is resolved against the node tree `Load` already parsed: walk from the root key by key, following aliases and merge keys the way the decoder does, and take the position of the last node reached. A value that comes in through an alias is located at the alias, which is where the user would edit it. If a key is missing, the walk stops at the deepest key that exists, which is where it would have to be added. A new `config.Check` reads, parses and validates in one step, keeping the tree for this; `Load` is unchanged for the commands that don't report problems.

Errors that come from parsing rather than validation are mapped one by one: yaml.v3's syntax errors carry `line N` in their text; its type errors list one message per bad value, each becoming its own problem; the placement errors for `!locked` and `!plain` already carry a line. Anything else becomes an unlocated error.

### The schema is written by hand and checked against the types

Generating the schema from the Go types by reflection was considered. It falls short in three ways here. Several types decode themselves and accept two shapes, which reflection can't see. The useful parts of a schema for an editor are the descriptions and the fixed sets of values, which aren't in the types. And generation would add a dependency to the build for one file.

So `internal/config/config.schema.json` is written by hand and embedded in the binary with `go:embed`. It is kept honest by tests:

- **Keys match.** A test walks the config types by reflection, using their `yaml` tags and a small table for the types that decode themselves, and compares the keys at each level with the schema's `properties`. A key added to a type and not to the schema fails the test, and so does the reverse.
- **The example is valid.** `rest-o-matic.example.yaml`, which lists every option, must validate against the schema.
- **Bad shapes are rejected.** A set of configs that `Load` rejects for their structure, such as a source with a key other than `paths`, or `notify` with an unknown kind, must also fail against the schema.

Draft-07 is used because yaml-language-server supports it fully; its 2019-09 and 2020-12 support is partial. `additionalProperties: false` throughout, except the top level, which allows `^x-` keys. The `$id` is stable across releases. A locked or `!plain` value is a string to the schema; the editor has to be told about the two tags (in monaco-yaml, `customTags: ["!locked scalar", "!plain scalar"]`), or it will flag them as unknown.

### Unknown keys are warnings

A misspelt key means part of the config isn't doing what its author thinks: `hook:` for `hooks:` silently skips a job's hooks. Each unknown key is reported as a warning with its line, suggesting the nearest known key in the same place when one is within two edits. `validate`, `tick` and `run` print config warnings already, so it is seen wherever the config is used, and the central app's editor marks it.

Three ways of handling an unknown key were considered:

- **An error.** Nothing runs with a config that means something other than what it says. But a config that works today would stop running after an upgrade until it was fixed, and a config error stops the whole tick, which no failure notification reports.
- **Treat a misspelt key as the key it resembles, and warn.** The hook would run, but on a guess. Renaming a block to switch it off (`nohooks:`, `_hooks:`) is common, and is within two edits of `hooks`, so commands someone meant to disable would run. A config would also mean one thing to the editor's schema and another to the host.
- **A warning, with the key still ignored.** Chosen: no working config stops, nothing runs on a guess, and the problem is reported every time the config is used.

The existing errors for unknown keys under `notify`, a job's `source` and `after` hooks stay errors, as they are today; their wording gains the same suggestion. That is inconsistent, but it changes nothing for a config that is valid today.

Keys under a repository's `env` are environment variable names and are not checked.

### Unknown keys are found using the schema

The warning for unknown keys and the schema must agree on what a known key is, or the editor and `validate` would disagree. So the check reads the embedded schema: it walks the node tree alongside the schema's `properties`, `patternProperties` and `additionalProperties`, picking the branch of a `oneOf` that matches the node's kind (a mapping or a list). Only that small part of JSON Schema is interpreted. A key with no place in the schema is warned about.

The schema itself rejects unknown keys (`additionalProperties: false`), so the editor shows them as problems straight away; the host is more lenient than the editor on purpose.

### `rest-o-matic schema`

A new top-level command, since the schema is about the config and not about validating one. It needs no config file. It prints the embedded file unchanged.

### The `validate` contract

`contract/validate/v1/` holds `validate-output.schema.json`, a README, and examples: valid, valid with a warning, invalid with located problems, a YAML syntax error, and a missing file. The UI repo tests its parsing against the same files.

## Risks / Trade-offs

- **[Trade-off] `read_as: podman-unshare` is checked against the machine validate runs on** (its OS, and whether podman is installed), as it is today. The central app runs on Linux, so it can miss the error a macOS host would give for that setting; the host still rejects it when it applies the config.
- **[Risk] The schema and the types drift.** → The key-matching test fails on any difference; the example and bad-shape tests catch wrong shapes.
- **[Trade-off] A warning, not an error, for unknown keys.** A misspelt `hooks` still means the hook doesn't run until someone acts on the warning.
- **[Risk] A key that is valid in a newer release is unknown to an older one, and ignored there.** → The central app validates with the version the host runs, so the editor shows the warning before the config is sent.
- **[Risk] A path that can't be resolved** (a problem raised about something derived, such as an effective schedule). → It is reported without a line rather than with a wrong one.
- **[Trade-off] A small JSON Schema interpreter in the host** for the unknown-key walk. → It handles only the constructs the schema uses, and a test fails if the schema uses one it doesn't know.

## Migration Plan

Nothing to migrate. Configs with unknown keys start printing warnings.

## Open Questions

None.
