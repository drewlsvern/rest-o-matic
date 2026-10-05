// Package contracttest checks messages against the check-in contract's
// JSON Schemas in contract/checkin/v1. It is for tests only.
package contracttest

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// baseID is the $id prefix every schema in the contract shares.
const baseID = "https://github.com/drewlsvern/rest-o-matic/contract/checkin/v1/"

// Dir is the contract folder.
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "contract", "checkin", "v1")
}

var (
	once     sync.Once
	compiler *jsonschema.Compiler
	loadErr  error
	schemas  = map[string]*jsonschema.Schema{}
	mu       sync.Mutex
)

func load() {
	compiler = jsonschema.NewCompiler()
	compiler.AssertFormat()
	paths, err := filepath.Glob(filepath.Join(Dir(), "*.schema.json"))
	if err != nil || len(paths) == 0 {
		loadErr = err
		return
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			loadErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			loadErr = err
			return
		}
		if loadErr = compiler.AddResource(baseID+filepath.Base(path), doc); loadErr != nil {
			return
		}
	}
}

// Validate fails the test unless data is valid against the named schema,
// such as "checkin-request".
func Validate(t *testing.T, schema string, data []byte) {
	t.Helper()
	once.Do(load)
	if loadErr != nil {
		t.Fatalf("loading the contract schemas: %v", loadErr)
	}
	mu.Lock()
	s, ok := schemas[schema]
	if !ok {
		var err error
		if s, err = compiler.Compile(baseID + schema + ".schema.json"); err != nil {
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
