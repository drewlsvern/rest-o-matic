# Validate output, format version 1

What `rest-o-matic validate --json` prints. The central app runs the
`rest-o-matic` binary of the version a host runs to check a config before
sending it there, and turns this document into markers in its editor.

| File | |
|---|---|
| `validate-output.schema.json` | The document's JSON Schema |
| `examples/` | Real output: a valid config, one with warnings, one with errors, a YAML syntax error, a missing file |

## Running it

```sh
rest-o-matic --config /tmp/candidate.yaml validate --json
```

- Exactly one JSON document is printed to standard output, whatever the
  outcome, including a file that can't be read or isn't YAML.
- Nothing is written to standard error unless the document itself couldn't
  be produced.
- The exit status is 0 when the config is valid (warnings allowed) and 1
  when it isn't.
- Validation needs nothing from the machine: no host key, state directory or
  source paths. Locked values are only checked for being well-formed. The
  one exception is `read_as: podman-unshare`, checked against the operating
  system and whether podman is installed.

## The document

```json
{
  "format_version": 1,
  "rest_o_matic_version": "v0.2.0",
  "valid": false,
  "problems": [
    {"severity": "error", "message": "references undefined policy \"hott\"",
     "job": "docs", "repository": null, "path": "backups.docs.policy", "line": 11, "column": 13}
  ]
}
```

- `valid` is true when no problem is an error.
- `problems` are sorted by line, then column. Problems with no line come
  first.
- Every problem has every key; a value that doesn't apply or isn't known is
  null.
- `message` is for a person, and doesn't repeat the job, repository or
  line.
- `path` is the dotted path of the field, with list positions as numbers:
  `backups.docs.repositories.1`.
- `line` and `column` are 1-based, and columns count characters. A missing
  key is placed at the nearest key above it that is present. A value taken
  from a YAML alias or merge key is placed at the alias, where it is used.
  An unknown key is placed at the key itself.

The format version changes only for an incompatible change. Adding a key is
not one, so ignore keys you don't know.
