// Package contracttest checks messages against the JSON Schemas of the
// contracts in contract/, such as contract/checkin/v1. It is for tests
// only.
package contracttest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Root is the folder holding every contract.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "contract")
}

// Dir is the check-in contract's folder.
func Dir() string { return filepath.Join(Root(), "checkin", "v1") }

var (
	once     sync.Once
	compiler *jsonschema.Compiler
	loadErr  error
	schemas  = map[string]*jsonschema.Schema{}
	// ids maps each schema's file name, without .schema.json, to its $id.
	// File names are unique across contracts.
	ids = map[string]string{}
	mu  sync.Mutex
)

func load() {
	compiler = jsonschema.NewCompiler()
	compiler.AssertFormat()
	paths, err := filepath.Glob(filepath.Join(Root(), "*", "v*", "*.schema.json"))
	if err != nil || len(paths) == 0 {
		loadErr = fmt.Errorf("no schemas found under %s: %v", Root(), err)
		return
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			loadErr = err
			return
		}
		var head struct {
			ID string `json:"$id"`
		}
		if err := json.Unmarshal(data, &head); err != nil || head.ID == "" {
			loadErr = fmt.Errorf("%s has no $id", path)
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			loadErr = err
			return
		}
		if loadErr = compiler.AddResource(head.ID, doc); loadErr != nil {
			return
		}
		name := strings.TrimSuffix(filepath.Base(path), ".schema.json")
		if _, dup := ids[name]; dup {
			loadErr = fmt.Errorf("two contracts have a schema named %s", name)
			return
		}
		ids[name] = head.ID
	}
}

// Validate fails the test unless data is valid against the named schema,
// such as "checkin-request" or "validate-output".
func Validate(t *testing.T, schema string, data []byte) {
	t.Helper()
	once.Do(load)
	if loadErr != nil {
		t.Fatalf("loading the contract schemas: %v", loadErr)
	}
	mu.Lock()
	s, ok := schemas[schema]
	if !ok {
		id, known := ids[schema]
		if !known {
			mu.Unlock()
			t.Fatalf("no contract schema named %s", schema)
		}
		var err error
		if s, err = compiler.Compile(id); err != nil {
			mu.Unlock()
			t.Fatalf("compiling schema %s: %v", schema, err)
		}
		schemas[schema] = s
	}
	mu.Unlock()

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: not JSON: %v", schema, err)
	}
	if err := s.Validate(doc); err != nil {
		t.Fatalf("message does not match %s.schema.json:\n%v\n\nmessage:\n%s", schema, err, clip(data))
	}
}

func clip(data []byte) string {
	s := string(data)
	if len(s) > 4000 {
		return s[:4000] + "\n…"
	}
	return strings.TrimSpace(s)
}
