package config

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/drewlsvern/rest-o-matic/internal/secrets"
)

// LockedValue is one locked value as written in a config file.
type LockedValue struct {
	// Text is the locked form, without the tag: exactly the characters
	// that appear in the file.
	Text string
	// Line is where the value is written. A value shared through an
	// anchor is written once, under the anchor.
	Line int
	// UsedAt is every place the value is used, such as
	// `repository "b2": password`, sorted.
	UsedAt []string
}

// LockedValues lists every distinct locked value used in a config file's
// repositories, in the order they are written. A value used in several
// places, through an anchor or because the same text was pasted twice, is
// listed once with all of them. Locked values outside the config sections
// that nothing uses are not listed.
func LockedValues(data []byte) ([]LockedValue, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if err := checkLockedPlacement(&root); err != nil {
		return nil, err
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil, nil
	}

	byText := map[string]*LockedValue{}
	var order []string
	add := func(n *yaml.Node, where string) {
		v, ok := byText[n.Value]
		if !ok {
			v = &LockedValue{Text: n.Value, Line: n.Line}
			byText[n.Value] = v
			order = append(order, n.Value)
		}
		for _, w := range v.UsedAt {
			if w == where {
				return
			}
		}
		v.UsedAt = append(v.UsedAt, where)
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "repositories" {
			continue
		}
		for _, repo := range mappingEntries(doc.Content[i+1], 0) {
			for _, field := range mappingEntries(repo.value, 0) {
				switch field.key {
				case "password":
					if n := resolve(field.value); n.Kind == yaml.ScalarNode && n.Tag == secrets.Tag {
						add(n, fmt.Sprintf("repository %q: password", repo.key))
					}
				case "env":
					for _, env := range mappingEntries(field.value, 0) {
						if n := resolve(env.value); n.Kind == yaml.ScalarNode && n.Tag == secrets.Tag {
							add(n, fmt.Sprintf("repository %q: env %s", repo.key, env.key))
						}
					}
				}
			}
		}
	}

	values := make([]LockedValue, 0, len(order))
	for _, text := range order {
		v := byText[text]
		sort.Strings(v.UsedAt)
		values = append(values, *v)
	}
	sort.SliceStable(values, func(i, j int) bool { return values[i].Line < values[j].Line })
	return values, nil
}

type entry struct {
	key   string
	value *yaml.Node
}

// mappingEntries returns a mapping's keys and values as the decoder sees
// them: aliases followed, and `<<` merge keys expanded, with the
// mapping's own keys winning over merged ones.
func mappingEntries(n *yaml.Node, depth int) []entry {
	n = resolve(n)
	if n.Kind != yaml.MappingNode || depth > maxAliasDepth {
		return nil
	}
	var own, merged []entry
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		if key.Tag == "!!merge" {
			value = resolve(value)
			sources := []*yaml.Node{value}
			if value.Kind == yaml.SequenceNode {
				sources = value.Content
			}
			for _, src := range sources {
				merged = append(merged, mappingEntries(src, depth+1)...)
			}
			continue
		}
		own = append(own, entry{key.Value, value})
	}
	seen := map[string]bool{}
	var out []entry
	for _, e := range append(own, merged...) {
		if !seen[e.key] {
			seen[e.key] = true
			out = append(out, e)
		}
	}
	return out
}

// resolve follows an alias to the node it names.
func resolve(n *yaml.Node) *yaml.Node {
	for i := 0; n.Kind == yaml.AliasNode && i <= maxAliasDepth; i++ {
		n = n.Alias
	}
	return n
}
