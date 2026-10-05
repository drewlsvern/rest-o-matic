package config

import _ "embed"

// Schema is a JSON Schema (draft-07) of the config file, for editors. It
// describes every key rest-o-matic reads and rejects any other, except
// top-level keys starting with x-. Tests keep it in step with the types
// in this package.
//
//go:embed config.schema.json
var Schema []byte
