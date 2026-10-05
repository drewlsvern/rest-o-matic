package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// schemaWalkKeywords are the JSON Schema keywords unknownKeys understands
// or can safely skip. A test fails if the schema uses any other, since the
// walk could then miss or wrongly report a key.
var schemaWalkKeywords = map[string]bool{
	// Followed by the walk.
	"$ref": true, "properties": true, "patternProperties": true, "additionalProperties": true,
	"items": true, "oneOf": true, "anyOf": true, "type": true, "definitions": true,
	// Constrain values only, which isn't the walk's business.
	"$schema": true, "$id": true, "title": true, "description": true, "enum": true,
	"minimum": true, "minItems": true, "required": true,
}

var (
	schemaOnce sync.Once
	schemaRoot map[string]any
)

func parsedSchema() map[string]any {
	schemaOnce.Do(func() {
		if err := json.Unmarshal(Schema, &schemaRoot); err != nil {
			panic("config.schema.json is not valid JSON: " + err.Error())
		}
	})
	return schemaRoot
}

// unknownKeys returns a warning for each key in the document that the
// schema has no place for, and so rest-o-matic doesn't read. Keys are
// checked where they are used: through aliases and merge keys, and not
// inside top-level x- keys, which only hold anchors.
func unknownKeys(root *yaml.Node) []Problem {
	if root == nil {
		return nil
	}
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	w := &keyWalk{schema: parsedSchema(), seen: map[*yaml.Node]bool{}}
	w.walk(doc, w.schema, nil, 0)
	sortProblems(w.problems)
	return w.problems
}

type keyWalk struct {
	schema   map[string]any
	seen     map[*yaml.Node]bool
	problems []Problem
}

func (w *keyWalk) deref(s map[string]any) map[string]any {
	for i := 0; i < maxAliasDepth; i++ {
		ref, ok := s["$ref"].(string)
		if !ok {
			return s
		}
		s = w.schema
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			s, _ = s[part].(map[string]any)
		}
	}
	return s
}

// branch picks, from a oneOf or anyOf, the schema for a node of kind.
func (w *keyWalk) branch(s map[string]any, kind yaml.Kind) map[string]any {
	s = w.deref(s)
	branches, ok := s["oneOf"].([]any)
	if !ok {
		branches, ok = s["anyOf"].([]any)
	}
	if !ok {
		return s
	}
	want := map[yaml.Kind]string{yaml.MappingNode: "object", yaml.SequenceNode: "array"}[kind]
	for _, b := range branches {
		b := w.deref(b.(map[string]any))
		if hasType(b, want) {
			return b
		}
	}
	return nil
}

func hasType(s map[string]any, want string) bool {
	switch t := s["type"].(type) {
	case string:
		return t == want
	case []any:
		for _, v := range t {
			if v == want {
				return true
			}
		}
	}
	return false
}

func (w *keyWalk) walk(n *yaml.Node, s map[string]any, path []string, depth int) {
	if n == nil || s == nil || depth > maxAliasDepth {
		return
	}
	n = resolve(n)
	s = w.branch(s, n.Kind)
	if s == nil {
		return
	}
	switch n.Kind {
	case yaml.SequenceNode:
		items, ok := s["items"].(map[string]any)
		if !ok {
			return
		}
		for i, item := range n.Content {
			w.walk(item, items, append(path[:len(path):len(path)], fmt.Sprint(i)), depth+1)
		}
	case yaml.MappingNode:
		props, _ := s["properties"].(map[string]any)
		patterns, _ := s["patternProperties"].(map[string]any)
		for _, e := range mappingEntries(n, 0) {
			child := append(path[:len(path):len(path)], e.key)
			if p, ok := props[e.key].(map[string]any); ok {
				w.walk(e.value, p, child, depth+1)
				continue
			}
			if p := matchPattern(patterns, e.key); p != nil {
				w.walk(e.value, p, child, depth+1)
				continue
			}
			switch extra := s["additionalProperties"].(type) {
			case map[string]any:
				w.walk(e.value, extra, child, depth+1)
			case bool:
				if !extra {
					w.report(e, child, props)
				}
			}
		}
	}
}

func matchPattern(patterns map[string]any, key string) map[string]any {
	for pattern, p := range patterns {
		if regexp.MustCompile(pattern).MatchString(key) {
			return p.(map[string]any)
		}
	}
	return nil
}

// report adds a warning for an unknown key, once however many places use
// it through anchors. It is placed at the key itself, which is what needs
// correcting.
func (w *keyWalk) report(e entry, path []string, known map[string]any) {
	if w.seen[e.keyNode] {
		return
	}
	w.seen[e.keyNode] = true
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	msg := fmt.Sprintf("%q is not a key rest-o-matic reads, and is ignored", e.key)
	if s := suggest(e.key, names); s != "" {
		msg += fmt.Sprintf("; did you mean %q?", s)
	}
	p := Problem{Severity: SeverityWarning, Message: msg, Path: strings.Join(path, "."), Line: e.keyNode.Line, Column: e.keyNode.Column}
	if len(path) >= 2 {
		switch path[0] {
		case "backups":
			p.Job = path[1]
		case "repositories":
			p.Repository = path[1]
		}
	}
	w.problems = append(w.problems, p)
}

// suggest returns the candidate closest in spelling to key, within two
// edits, or "" if none is.
func suggest(key string, candidates []string) string {
	sort.Strings(candidates)
	best, bestDist := "", 3
	for _, c := range candidates {
		if d := editDistance(key, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

// didYouMean is "; did you mean ...?" naming the candidate closest to key,
// or "" when none is close.
func didYouMean(key string, candidates ...string) string {
	if s := suggest(key, candidates); s != "" {
		return fmt.Sprintf("; did you mean %q?", s)
	}
	return ""
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
