package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/drewlsvern/rest-o-matic/internal/secrets"
)

// DefaultMaxConcurrent is used when a config file doesn't set max_concurrent.
const DefaultMaxConcurrent = 2

// Load reads and parses a config file at path, filling in each Job's Name
// from its map key and applying defaults (currently just MaxConcurrent).
// It does not validate cross-references - call Validate for that.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	// Parsed to a node tree first, so that a locked value in a place that
	// can't hold one is caught: decoding alone would quietly accept it as
	// the text of an ordinary field.
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if err := checkLockedPlacement(&root); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	var cfg Config
	if root.Kind != 0 { // an empty file has no document node to decode
		if err := root.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	}

	for name, job := range cfg.Backups {
		job.Name = name
		cfg.Backups[name] = job
	}

	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = DefaultMaxConcurrent
	}

	return &cfg, nil
}

// configSections are the top-level keys the config is read from. Anything
// else at the top level (an `x-...` holder for YAML anchors, say) is not
// part of the config, and only matters where it is aliased into one of
// these.
var configSections = map[string]bool{"max_concurrent": true, "policies": true, "repositories": true, "backups": true, "notify": true}

// checkLockedPlacement rejects a locked value anywhere other than a
// repository's password or one of its env values. Aliases and merge keys
// are followed, so a locked value shared through an anchor is judged by
// where it is used, not where it is defined.
func checkLockedPlacement(root *yaml.Node) error {
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key, value := doc.Content[i], doc.Content[i+1]
		if !configSections[key.Value] {
			continue
		}
		if err := walkLocked(value, []string{key.Value}, 0); err != nil {
			return err
		}
	}
	return nil
}

// maxAliasDepth stops a self-referencing anchor from recursing forever.
const maxAliasDepth = 64

func walkLocked(n *yaml.Node, path []string, depth int) error {
	if depth > maxAliasDepth {
		return fmt.Errorf("line %d: anchors are nested too deeply", n.Line)
	}
	switch n.Kind {
	case yaml.AliasNode:
		return walkLocked(n.Alias, path, depth+1)
	case yaml.ScalarNode:
		if n.Tag == secrets.Tag && !lockable(path) {
			return fmt.Errorf("line %d: %s is only allowed on a repository's password and env values, not on %s",
				n.Line, secrets.Tag, strings.Join(path, "."))
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			if err := walkLocked(item, append(path[:len(path):len(path)], strconv.Itoa(i)), depth+1); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			child := append(path[:len(path):len(path)], key.Value)
			if key.Tag == "!!merge" {
				// `<<: *anchor` puts the anchor's keys here, so they take
				// this mapping's path, not one with `<<` in it.
				child = path
			}
			if err := walkLocked(value, child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// lockable reports whether path is repositories.<name>.password or
// repositories.<name>.env.<NAME>.
func lockable(path []string) bool {
	if len(path) < 3 || path[0] != "repositories" {
		return false
	}
	return (len(path) == 3 && path[2] == "password") || (len(path) == 4 && path[2] == "env")
}
