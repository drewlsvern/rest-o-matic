//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/central/contracttest"
	"github.com/drewlsvern/rest-o-matic/internal/config"
)

const validConfig = `policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/nas, password_file: /etc/restic-pw}
backups:
  docs:
    source: {paths: [/home/me/docs]}
    policy: hot
    repositories: [nas]
`

func TestCLI_ValidateJSON(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cases := []struct {
		name, config string
		code         int
		valid        bool
		problems     []string // severity:path:line for each problem
	}{
		{"valid", write("valid.yaml", validConfig), 0, true, nil},
		{"warnings", write("warn.yaml", strings.Replace(validConfig, "    policy: hot", "    policy: hot\n    hook: {before: [pg_dump]}", 1)), 0, true,
			[]string{"warning:backups.docs.hook:9"}},
		{"errors", write("bad.yaml", strings.Replace(validConfig, "policy: hot", "policy: hott", 1)), 1, false,
			[]string{"error:backups.docs.policy:8"}},
		{"syntax error", write("syntax.yaml", "policies:\n  hot: {schedule: hourly\nrepositories:\n"), 1, false,
			[]string{"error::2"}},
		{"missing file", filepath.Join(dir, "missing.yaml"), 1, false, []string{"error::0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A key file and state directory that don't exist: validate must
			// need neither, and create neither.
			keyFile, stateDir := filepath.Join(dir, "no-keys", "host.key"), filepath.Join(dir, "no-state")
			stdout, stderr, code := runCLI(t, bin, dir, "--config", c.config, "--key-file", keyFile, "--state-dir", stateDir, "validate", "--json")
			if code != c.code || stderr != "" {
				t.Fatalf("exit %d (want %d), stderr %q", code, c.code, stderr)
			}
			contracttest.Validate(t, "validate-output", []byte(stdout))
			var doc validateDoc
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range doc.Problems {
				path, line := "", 0
				if p.Path != nil {
					path = *p.Path
				}
				if p.Line != nil {
					line = *p.Line
				}
				got = append(got, p.Severity+":"+path+":"+itoa(line))
			}
			if doc.Valid != c.valid || strings.Join(got, ",") != strings.Join(c.problems, ",") {
				t.Errorf("got valid=%v %v, want valid=%v %v", doc.Valid, got, c.valid, c.problems)
			}
			for _, p := range []string{keyFile, filepath.Dir(keyFile), stateDir} {
				if _, err := os.Stat(p); err == nil {
					t.Errorf("validate created %s", p)
				}
			}
		})
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestCLI_ValidateTextHasLines(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	os.WriteFile(path, []byte(strings.Replace(strings.Replace(validConfig, "policy: hot", "policy: hott", 1), "    source:", "    hook: {before: [x]}\n    source:", 1)), 0o644)
	_, stderr, code := runCLI(t, bin, dir, "--config", path, "validate")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		`config warning: line 7: job "docs": "hook" is not a key rest-o-matic reads, and is ignored; did you mean "hooks"?`,
		`config error: line 9: job "docs": references undefined policy "hott"`,
		"1 config validation error(s)",
	} {
		if !contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Index(stderr, "config warning") > strings.Index(stderr, "config error") {
		t.Error("warnings should come before errors")
	}
}

func TestCLI_Schema(t *testing.T) {
	bin := buildBinary(t)
	// No config file anywhere: schema doesn't need one.
	stdout, stderr, code := runCLI(t, bin, t.TempDir(), "schema")
	if code != 0 || stdout != string(config.Schema) {
		t.Fatalf("schema (exit %d): %s", code, stderr)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil || v["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("not a draft-07 schema: %v", err)
	}
}

// An unknown key is a warning: the tick still runs its jobs.
func TestCLI_TickRunsWithUnknownKey(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	workdir, configPath := setupWorkspace(t)
	data, _ := os.ReadFile(configPath)
	os.WriteFile(configPath, []byte(strings.Replace(string(data), "    policy: hot", "    policy: hot\n    hook:\n      before: [\"touch "+filepath.Join(workdir, "ran")+"\"]", 1)), 0o644)

	stdout, stderr, code := runCLI(t, bin, workdir, "--config", configPath, "tick")
	if code != 0 || !contains(stdout, "1 succeeded") {
		t.Fatalf("tick (exit %d): %s %s", code, stdout, stderr)
	}
	if !contains(stderr, `config warning: line`) || !contains(stderr, `did you mean "hooks"?`) {
		t.Errorf("no warning for the unknown key: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(workdir, "ran")); err == nil {
		t.Error("the misspelt hook ran")
	}
}
