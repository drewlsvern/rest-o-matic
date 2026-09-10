// Package config defines the rest-o-matic configuration schema: Source,
// Repository, Policy, Hooks, and the backup jobs that compose them.
package config

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// Config is the top-level rest-o-matic configuration file.
type Config struct {
	// MaxConcurrent bounds how many job executions may run at the same time.
	// Defaults to 2 when unset (see Load).
	MaxConcurrent int `yaml:"max_concurrent"`

	Policies     map[string]Policy     `yaml:"policies"`
	Repositories map[string]Repository `yaml:"repositories"`
	Backups      map[string]Job        `yaml:"backups"`
}

// Retention maps a restic keep-period name (hourly, daily, weekly, monthly,
// yearly) to how many snapshots of that period to keep.
type Retention map[string]int

// Policy is a named, reusable schedule + retention pair.
type Policy struct {
	Schedule  string    `yaml:"schedule"`
	Retention Retention `yaml:"retention"`
}

// Repository describes where a restic repository lives. URL is passed
// directly to restic's -r flag, so any restic-supported backend (local,
// s3, sftp, rest-server, b2, azure, ...) is accepted uniformly - rest-o-matic
// does no backend-specific validation of it.
type Repository struct {
	Backend string `yaml:"backend"`
	URL     string `yaml:"url"`

	Password        string `yaml:"password,omitempty"`
	PasswordFile    string `yaml:"password_file,omitempty"`
	PasswordCommand string `yaml:"password_command,omitempty"`

	// Env carries arbitrary backend-specific credentials (e.g.
	// AWS_ACCESS_KEY_ID for an s3 backend) through to the restic process.
	Env map[string]string `yaml:"env,omitempty"`
}

// Source declares what a job backs up. In this version only a `paths`
// source is supported; any other shape is a config error.
type Source struct {
	Paths []string
}

// UnmarshalYAML enforces that a Source declares only `paths`, rejecting any
// other source type (container, quadlet, command, ...) since those are not
// supported yet.
func (s *Source) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]yaml.Node
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	var unsupported []string
	for k := range raw {
		if k != "paths" {
			unsupported = append(unsupported, k)
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return fmt.Errorf("source: unsupported source type(s) %v; only 'paths' is supported in this version", unsupported)
	}

	if pathsNode, ok := raw["paths"]; ok {
		var paths []string
		if err := pathsNode.Decode(&paths); err != nil {
			return fmt.Errorf("source.paths: %w", err)
		}
		s.Paths = paths
	}
	return nil
}

// Hooks are shell commands run once per job execution.
type Hooks struct {
	Before []string `yaml:"before"`
	After  []string `yaml:"after"`
}

// RepositoryRef is one entry in a job's repositories list. It may be a bare
// repository name, or a mapping with `repo` plus a per-repository retention
// override.
type RepositoryRef struct {
	Name      string
	Retention Retention
}

// UnmarshalYAML accepts either a plain scalar (`- nas`) or a mapping
// (`- repo: offsite` with an optional `retention` override).
func (r *RepositoryRef) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var name string
		if err := value.Decode(&name); err != nil {
			return fmt.Errorf("repository entry: %w", err)
		}
		r.Name = name
		return nil
	case yaml.MappingNode:
		var raw struct {
			Repo      string    `yaml:"repo"`
			Retention Retention `yaml:"retention"`
		}
		if err := value.Decode(&raw); err != nil {
			return fmt.Errorf("repository entry: %w", err)
		}
		if raw.Repo == "" {
			return fmt.Errorf("repository entry: mapping form requires a 'repo' name")
		}
		r.Name = raw.Repo
		r.Retention = raw.Retention
		return nil
	default:
		return fmt.Errorf("repository entry: unsupported YAML shape")
	}
}

// Job is a single named backup job: what to back up (Source), where
// (Repositories), how/when (Policy, with optional overrides), and what to
// run around it (Hooks).
type Job struct {
	// Name is populated from the `backups` map key after loading, not from
	// YAML content.
	Name string `yaml:"-"`

	Source Source `yaml:"source"`
	Hooks  Hooks  `yaml:"hooks"`

	Policy    string    `yaml:"policy"`
	Retention Retention `yaml:"retention"`

	Repositories []RepositoryRef `yaml:"repositories"`

	// Tags are additive: they never replace the automatic job-name tag
	// applied to every snapshot.
	Tags []string `yaml:"tags"`
}
