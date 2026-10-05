package config

import "sort"

// harmlessEnv are env names whose values are settings, not secrets, so
// they may stay in plain text without keeping the config from the central
// app. Every other name counts as a secret, account identifiers included.
var harmlessEnv = map[string]bool{
	"AWS_DEFAULT_REGION":      true,
	"AWS_REGION":              true,
	"RESTIC_COMPRESSION":      true,
	"RESTIC_PACK_SIZE":        true,
	"RESTIC_READ_CONCURRENCY": true,
	"RESTIC_CACHE_DIR":        true,
	"TMPDIR":                  true,
	"GOMAXPROCS":              true,
}

// HarmlessEnvNames lists the env names that may be plain text, sorted.
func HarmlessEnvNames() []string {
	names := make([]string, 0, len(harmlessEnv))
	for name := range harmlessEnv {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// PlainTextSecrets lists every repository password and env value written
// in plain text that may be a secret, as dotted paths such as
// repositories.nas.password, sorted. An env value marked `!plain`, or
// whose name is a known harmless setting, is not listed.
func PlainTextSecrets(cfg *Config) []string {
	var fields []string
	for name, repo := range cfg.Repositories {
		if !repo.Password.IsZero() && !repo.Password.Locked {
			fields = append(fields, "repositories."+name+".password")
		}
		for envName, v := range repo.Env {
			if v.Locked || v.MarkedPlain || harmlessEnv[envName] {
				continue
			}
			fields = append(fields, "repositories."+name+".env."+envName)
		}
	}
	sort.Strings(fields)
	return fields
}
