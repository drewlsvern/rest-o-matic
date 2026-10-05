package config

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func compiledSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(Schema))
	if err != nil {
		t.Fatalf("the schema is not JSON: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("config.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("config.schema.json")
	if err != nil {
		t.Fatalf("compiling the schema: %v", err)
	}
	return s
}

// schemaErr checks a YAML config against the schema, as an editor would.
func schemaErr(t *testing.T, s *jsonschema.Schema, content string) error {
	t.Helper()
	var v any
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		t.Fatalf("not YAML: %v", err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return s.Validate(doc)
}

// rawSchema is the schema as plain JSON values, for walking.
func rawSchema(t *testing.T) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(Schema, &root); err != nil {
		t.Fatal(err)
	}
	return root
}

// deref follows a local $ref.
func deref(root, node map[string]any) map[string]any {
	for {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		node = root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			node = node[part].(map[string]any)
		}
	}
}

// objectBranch returns the schema for the mapping form of a node: the node
// itself, or the branch of its oneOf that is an object.
func objectBranch(root, node map[string]any) map[string]any {
	node = deref(root, node)
	if branches, ok := node["oneOf"].([]any); ok {
		for _, b := range branches {
			if b := deref(root, b.(map[string]any)); b["type"] == "object" {
				return b
			}
		}
	}
	return node
}

func propertyNames(node map[string]any) []string {
	props, _ := node["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// customKeys are the keys of the types that decode themselves, which
// reflection can't see, by the field type they decode into.
var customKeys = map[reflect.Type]map[string]reflect.Type{
	reflect.TypeOf(Source{}):        {"paths": reflect.TypeOf([]string{})},
	reflect.TypeOf(Notify{}):        {"failure": reflect.TypeOf([]string{}), "recovery": reflect.TypeOf([]string{}), "success": reflect.TypeOf([]string{})},
	reflect.TypeOf(AfterHooks{}):    {"always": reflect.TypeOf([]string{}), "success": reflect.TypeOf([]string{}), "failure": reflect.TypeOf([]string{})},
	reflect.TypeOf(RepositoryRef{}): {"repo": reflect.TypeOf(""), "retention": reflect.TypeOf(Retention{})},
}

// goKeys returns the keys a struct type reads, with the type each decodes
// into.
func goKeys(typ reflect.Type) map[string]reflect.Type {
	if keys, ok := customKeys[typ]; ok {
		return keys
	}
	keys := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" || name == "-" || !f.IsExported() {
			continue
		}
		keys[name] = f.Type
	}
	return keys
}

// compareKeys checks, at every level, that the schema's keys are exactly
// the keys the Go types read.
func compareKeys(t *testing.T, root, node map[string]any, typ reflect.Type, where string) {
	t.Helper()
	switch {
	case typ == reflect.TypeOf(Secret{}):
		return // a string to the schema
	case typ == reflect.TypeOf(Retention{}):
		node = objectBranch(root, node)
		want := append([]string(nil), RetentionPeriods...)
		sort.Strings(want)
		if got := propertyNames(node); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: schema has periods %v, rest-o-matic reads %v", where, got, want)
		}
		return
	case typ.Kind() == reflect.Map:
		node = deref(root, node)
		items, ok := node["additionalProperties"].(map[string]any)
		if !ok {
			t.Errorf("%s: a map in the types, but the schema has no additionalProperties schema", where)
			return
		}
		compareKeys(t, root, items, typ.Elem(), where+".*")
		return
	case typ.Kind() == reflect.Slice:
		node = deref(root, node)
		if branches, ok := node["oneOf"].([]any); ok { // a list, or another form
			for _, b := range branches {
				if b := deref(root, b.(map[string]any)); b["type"] == "array" {
					node = b
				}
			}
		}
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Errorf("%s: a list in the types, but the schema has no items", where)
			return
		}
		compareKeys(t, root, items, typ.Elem(), where+".0")
		return
	case typ.Kind() != reflect.Struct:
		return
	}

	node = objectBranch(root, node)
	keys := goKeys(typ)
	want := make([]string, 0, len(keys))
	for k := range keys {
		want = append(want, k)
	}
	sort.Strings(want)
	if got := propertyNames(node); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: schema has keys %v, rest-o-matic reads %v", where, got, want)
		return
	}
	if node["additionalProperties"] != false {
		t.Errorf("%s: the schema does not reject unknown keys", where)
	}
	props := node["properties"].(map[string]any)
	for name, fieldType := range keys {
		compareKeys(t, root, props[name].(map[string]any), fieldType, where+"."+name)
	}
}

func TestSchema_KeysMatchTheTypes(t *testing.T) {
	root := rawSchema(t)
	compareKeys(t, root, root, reflect.TypeOf(Config{}), "(top)")

	props := root["properties"].(map[string]any)
	for _, required := range []struct{ def, key string }{{"repository", "backend"}, {"job", "policy"}} {
		def := root["definitions"].(map[string]any)[required.def].(map[string]any)
		if !strings.Contains(strings.Join(anyStrings(def["required"]), ","), required.key) {
			t.Errorf("%s.%s should be required", required.def, required.key)
		}
	}
	for name, p := range props {
		if _, ok := p.(map[string]any)["description"]; !ok && deref(root, p.(map[string]any))["description"] == nil {
			t.Errorf("top-level %s has no description", name)
		}
	}
}

func anyStrings(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

// uncommentExample turns the commented-out example config into the config
// it shows: a line such as `# policies:` opens a section, a `# ---` divider
// closes it, and inside a section one level of `# ` is removed.
func uncommentExample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../rest-o-matic.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	section := regexp.MustCompile(`^# [a-z_]+:`)
	var out []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case section.MatchString(line):
			in = true
			out = append(out, line[2:])
		case strings.HasPrefix(line, "# ---"):
			in = false
		case in && strings.HasPrefix(line, "#  "):
			out = append(out, line[2:])
		}
	}
	return strings.Join(out, "\n") + "\n"
}

func TestSchema_ExampleConfigIsValid(t *testing.T) {
	content := uncommentExample(t)
	cfg, problems := Check(writeTempConfig(t, content))
	if cfg == nil || len(cfg.Backups) < 3 || len(cfg.Repositories) < 3 || HasErrors(problems) {
		t.Fatalf("the uncommented example doesn't load cleanly (%d jobs): %+v\n%s", len(cfg.Backups), problems, content)
	}
	if err := schemaErr(t, compiledSchema(t), content); err != nil {
		t.Fatalf("the example config is not valid against the schema: %v", err)
	}
}

// Configs rest-o-matic accepts are accepted by the schema too.
func TestSchema_AcceptsWhatTheHostAccepts(t *testing.T) {
	s := compiledSchema(t)
	for name, content := range map[string]string{
		"anchors and locked values": `
x-shared:
  pw: &pw !locked "YWdl"
  s3: &s3 {backend: s3, env: {AWS_DEFAULT_REGION: eu-west-1, MY_PREFIX: !plain "a/"}}
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/nas, password: *pw}
  offsite:
    <<: *s3
    url: "s3:https://s3.example.com/b"
    password_command: pass show x
backups:
  docs:
    source: {paths: ["~/docs"]}
    policy: hot
    repositories: [nas, {repo: offsite, retention: {daily: 3}}]
    hooks: {before: [echo], after: [echo]}
`,
		"unrecognised backend": `
repositories:
  r: {backend: something-new, url: "x:y"}
`,
		"empty file":     ``,
		"empty sections": "policies:\nrepositories:\nbackups:\nnotify:\n",
	} {
		if err := schemaErr(t, s, content); err != nil {
			t.Errorf("%s: rejected by the schema: %v", name, err)
		}
	}
}

// Configs rest-o-matic rejects for their structure are rejected by the
// schema too.
func TestSchema_RejectsBadShapes(t *testing.T) {
	s := compiledSchema(t)
	base := "policies:\n  hot: {schedule: hourly}\nrepositories:\n  nas: {backend: local, url: /srv/nas}\n"
	for name, content := range map[string]string{
		"source with another key":     base + "backups:\n  docs: {source: {paths: [/a], container: x}, policy: hot, repositories: [nas]}\n",
		"unknown notify kind":         "notify: {sometimes: [echo]}\n",
		"unknown after outcome":       base + "backups:\n  docs: {source: {paths: [/a]}, policy: hot, repositories: [nas], hooks: {after: {maybe: [echo]}}}\n",
		"repository entry, no repo":   base + "backups:\n  docs: {source: {paths: [/a]}, policy: hot, repositories: [{retention: {daily: 1}}]}\n",
		"max_concurrent not a number": "max_concurrent: lots\n",
		"missing policy":              base + "backups:\n  docs: {source: {paths: [/a]}, repositories: [nas]}\n",
		"empty source":                base + "backups:\n  docs: {source: {paths: []}, policy: hot, repositories: [nas]}\n",
		"unknown read_as":             base + "backups:\n  docs: {source: {paths: [/a]}, policy: hot, repositories: [nas], read_as: sudo}\n",
		"misspelt key":                base + "backups:\n  docs: {source: {paths: [/a]}, policy: hot, repositories: [nas], hook: {before: [echo]}}\n",
		"misspelt period":             "policies:\n  hot: {schedule: hourly, retention: {dayly: 7}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := schemaErr(t, s, content); err == nil {
				t.Error("accepted by the schema")
			}
			// Everything here but the two misspellings is rejected by the
			// host too; those are warnings there.
			if _, problems := Check(writeTempConfig(t, content)); !HasErrors(problems) && !strings.HasPrefix(name, "misspelt") {
				t.Errorf("accepted by rest-o-matic: %+v", problems)
			}
		})
	}
}
