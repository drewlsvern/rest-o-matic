package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
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

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
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
