## 1. Paths and Locations

- [x] 1.1 `ValidationError` and `Warning` gain `Path`; set it at every place one is created
- [x] 1.2 `Load` keeps the parsed node tree; a resolver maps a dotted path to a line and column, following aliases and merge keys, falling back to the deepest key present
- [x] 1.3 Parse errors become problems: YAML syntax errors with their line, each type error separately, the `!locked`/`!plain` placement errors with theirs, anything else unlocated
- [x] 1.4 Text output prefixes located problems with `line N:`
- [x] 1.5 Tests: an undefined policy, a missing key, a value through an alias and through a merge key, a syntax error, a type error, an unreadable file

## 2. The Config Schema

- [x] 2.1 `internal/config/config.schema.json` (draft-07): every key with a description, fixed value sets, both forms of repository references and `after` hooks, `additionalProperties: false`, `^x-` allowed at the top level; embedded
- [x] 2.2 `rest-o-matic schema` prints it
- [x] 2.3 Tests: keys match the config types by reflection, both ways; `rest-o-matic.example.yaml` is valid; a set of structurally bad configs that `Load` rejects are invalid

## 3. Unknown Keys

- [x] 3.1 A walk of the node tree against the embedded schema (`properties`, `patternProperties`, `additionalProperties`, `oneOf` by node kind), giving a config warning for each unknown key with its path and line, and a suggestion within two edits; `env` names are not checked
- [x] 3.2 Leave the existing errors for unknown keys under `notify`, `source` and `after` as errors, adding the same suggestion to their wording
- [x] 3.3 Tests: `hook`, `retension` and `pasword_file` are each warned about with a suggestion and their line; `nohooks` holding a `before` list is warned about and its commands never run; `x-shared` and `env` names are not; a key under a merged anchor is checked where it is used; `tick` runs its jobs and prints the warning; the walk fails a test if the schema uses a construct it doesn't handle

## 4. JSON Output and Contract

- [x] 4.1 `validate --json`: one document on standard output in every case, problems sorted by line and column, exit status unchanged
- [x] 4.2 `contract/validate/v1/`: README, `validate-output.schema.json`, examples (valid, valid with a warning, invalid with located problems, syntax error, missing file)
- [x] 4.3 Tests: the examples and real output for each case validate against the contract schema; nothing on standard error; validation reads no host key or state directory

## 5. Docs and Verification

- [x] 5.1 `docs/configuration.md`: unknown keys are warned about, `x-` keys, and using the schema in an editor (VS Code's YAML extension, monaco-yaml, with the custom tags)
- [x] 5.2 `docs/cli.md` and the README command table: `validate --json`, `schema`
- [x] 5.3 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...`, `GOOS=darwin go build ./...` pass; `openspec validate validate-json-and-schema --type change --strict` passes
