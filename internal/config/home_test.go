package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const homeConfig = `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: ~/restic-repo, password: x}
backups:
  documents:
    source:
      paths: [%s]
    policy: hot
    repositories: [nas]
`

func loadWithPaths(t *testing.T, paths string) (*Config, error) {
	t.Helper()
	return Load(writeTempConfig(t, strings.Replace(homeConfig, "%s", paths, 1)))
}

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	orig := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = orig })
	return home
}

func TestLoad_ExpandsLeadingTildeInSourcePaths(t *testing.T) {
	home := setHome(t)

	cfg, err := loadWithPaths(t, `"~/documents", "~", "~/a/b"`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{filepath.Join(home, "documents"), home, filepath.Join(home, "a", "b")}
	got := cfg.Backups["documents"].Source.Paths
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got paths %v, want %v", got, want)
	}
}

func TestLoad_LeavesOtherTildesAndVariablesAlone(t *testing.T) {
	setHome(t)

	cfg, err := loadWithPaths(t, `"~alice/documents", "/srv/~backup", "/data", "$HOME/x", "relative/~"`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "~alice/documents|/srv/~backup|/data|$HOME/x|relative/~"
	if got := strings.Join(cfg.Backups["documents"].Source.Paths, "|"); got != want {
		t.Fatalf("got paths %q, want them exactly as written: %q", got, want)
	}
}

func TestLoad_RepositoryURLIsNotExpanded(t *testing.T) {
	setHome(t)

	cfg, err := loadWithPaths(t, `"/data"`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Repositories["nas"].URL; got != "~/restic-repo" {
		t.Fatalf("got url %q, want it passed through exactly as written", got)
	}
}

func TestLoad_UnknownHomeDirectoryIsAnError(t *testing.T) {
	orig := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("$HOME is not defined") }
	t.Cleanup(func() { userHomeDir = orig })

	_, err := loadWithPaths(t, `"~/documents"`)
	if err == nil {
		t.Fatal("expected loading to fail when ~ can't be expanded")
	}
	for _, want := range []string{`job "documents"`, `"~/documents"`, "$HOME is not defined"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("got %q, want it to contain %q", err, want)
		}
	}

	// A config that doesn't use ~ never needs the home directory.
	if _, err := loadWithPaths(t, `"/data"`); err != nil {
		t.Fatalf("a config with no ~ failed to load without a home directory: %v", err)
	}
}
