package contracttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every example in the contract matches the schema its name says.
func TestExamplesMatchSchemas(t *testing.T) {
	schemaFor := map[string]string{
		"enrol-request":    "enrol-request",
		"enrol-response":   "enrol-response",
		"checkin-response": "checkin-response",
		"checkin":          "checkin-request",
		"error":            "error",
	}
	paths, _ := filepath.Glob(filepath.Join(Dir(), "examples", "*.json"))
	if len(paths) < 9 {
		t.Fatalf("found %d examples, expected at least 9", len(paths))
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		// The longest matching prefix decides: checkin-response before checkin.
		best := ""
		for prefix := range schemaFor {
			if strings.HasPrefix(name, prefix) && len(prefix) > len(best) {
				best = prefix
			}
		}
		schema := schemaFor[best]
		if schema == "" {
			t.Errorf("%s: no schema for this example name", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			Validate(t, schema, data)
		})
	}
}

func TestValidateExamplesMatchSchema(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join(Root(), "validate", "v1", "examples", "*.json"))
	if len(paths) < 5 {
		t.Fatalf("found %d examples, expected at least 5", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			Validate(t, "validate-output", data)
		})
	}
}
