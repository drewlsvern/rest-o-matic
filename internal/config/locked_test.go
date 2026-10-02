package config

import (
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/drewlsvern/rest-o-matic/internal/secrets"
)

// lockedValue returns a well-formed locked value. No test here unlocks it:
// loading and validating a config must never need a key.
func lockedValue(t *testing.T) string {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "host.key")
	if _, err := secrets.Generate(keyPath); err != nil {
		t.Fatal(err)
	}
	key, err := secrets.LoadHostKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := secrets.Lock("s3cret", []age.Recipient{key.Recipient()})
	if err != nil {
		t.Fatal(err)
	}
	return locked
}

const lockedConfigRest = `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [offsite]
`

func TestLoad_LockedAndPlainCredentialsMixed(t *testing.T) {
	locked := lockedValue(t)
	path := writeTempConfig(t, `
repositories:
  offsite:
    backend: s3
    url: "s3:https://s3.example.com/bucket/repo"
    password: !locked "`+locked+`"
    env:
      AWS_ACCESS_KEY_ID: AKIAEXAMPLE
      AWS_SECRET_ACCESS_KEY: !locked "`+locked+`"
`+lockedConfigRest)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repo := cfg.Repositories["offsite"]
	if !repo.Password.Locked || repo.Password.Value != locked {
		t.Errorf("got password %+v, want the locked value", repo.Password)
	}
	if got := repo.Env["AWS_ACCESS_KEY_ID"]; got != Plain("AKIAEXAMPLE") {
		t.Errorf("got AWS_ACCESS_KEY_ID %+v, want it plain", got)
	}
	if got := repo.Env["AWS_SECRET_ACCESS_KEY"]; !got.Locked {
		t.Errorf("got AWS_SECRET_ACCESS_KEY %+v, want it locked", got)
	}
	if res := Validate(cfg); len(res.Errors) != 0 {
		t.Errorf("a config with well-formed locked values failed validation: %v", res.Errors)
	}
}

func TestLoad_PlainCredentialsAreNotLocked(t *testing.T) {
	path := writeTempConfig(t, `
repositories:
  offsite:
    backend: local
    url: /srv/repo
    password: correct-horse
`+lockedConfigRest)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Repositories["offsite"].Password; got != Plain("correct-horse") {
		t.Fatalf("got password %+v, want it plain and unchanged", got)
	}
}

func TestLoad_LockedValueOnAnotherFieldIsRejected(t *testing.T) {
	locked := lockedValue(t)
	cases := map[string]struct{ config, where string }{
		"repository url": {`
repositories:
  offsite:
    backend: local
    url: !locked "` + locked + `"
` + lockedConfigRest, "repositories.offsite.url"},
		"password_command": {`
repositories:
  offsite:
    backend: local
    url: /srv/repo
    password_command: !locked "` + locked + `"
` + lockedConfigRest, "repositories.offsite.password_command"},
		"hook": {`
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  offsite: {backend: local, url: /srv/repo, password: x}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [offsite]
    hooks:
      before:
        - echo one
        - !locked "` + locked + `"
`, "backups.documents.hooks.before.1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, c.config))
			if err == nil {
				t.Fatal("expected the config to be rejected")
			}
			if !strings.Contains(err.Error(), c.where) || !strings.Contains(err.Error(), "line ") {
				t.Fatalf("got %v, want an error naming %s and its line", err, c.where)
			}
		})
	}
}

// A locked value shared through an anchor is judged by where it is used.
func TestLoad_LockedValueThroughAnchors(t *testing.T) {
	locked := lockedValue(t)

	t.Run("alias and merge key in allowed places", func(t *testing.T) {
		cfg, err := Load(writeTempConfig(t, `
x-shared:
  password: &pw !locked "`+locked+`"
  s3: &s3
    backend: s3
    env:
      AWS_SECRET_ACCESS_KEY: !locked "`+locked+`"
repositories:
  nas: {backend: local, url: /srv/repo, password: *pw}
  offsite:
    <<: *s3
    url: "s3:https://s3.example.com/bucket/repo"
    password: *pw
`+lockedConfigRest))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.Repositories["nas"].Password.Locked || !cfg.Repositories["offsite"].Password.Locked {
			t.Error("the aliased password was not read as locked")
		}
		if !cfg.Repositories["offsite"].Env["AWS_SECRET_ACCESS_KEY"].Locked {
			t.Error("the merged env value was not read as locked")
		}
	})

	t.Run("alias in a place that can't be locked", func(t *testing.T) {
		_, err := Load(writeTempConfig(t, `
x-shared:
  password: &pw !locked "`+locked+`"
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/repo, password: *pw}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [nas]
    tags: [*pw]
`))
		if err == nil || !strings.Contains(err.Error(), "backups.documents.tags.0") {
			t.Fatalf("got %v, want the use under tags rejected", err)
		}
	})
}

func TestValidate_MalformedLockedValue(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, `
repositories:
  offsite:
    backend: local
    url: /srv/repo
    password: !locked "not a locked value"
    env:
      TOKEN: !locked "aGVsbG8="
      PLAIN: !locked ""
`+lockedConfigRest))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var messages []string
	for _, e := range Validate(cfg).Errors {
		messages = append(messages, e.Error())
	}
	all := strings.Join(messages, "\n")
	for _, want := range []string{`repository "offsite": password is marked !locked`, "env TOKEN is marked !locked", "env PLAIN is marked !locked"} {
		if !strings.Contains(all, want) {
			t.Errorf("expected an error containing %q, got:\n%s", want, all)
		}
	}
}

// Validation reads no key, so it gives the same answer on any machine.
func TestValidate_NeedsNoHostKey(t *testing.T) {
	locked := lockedValue(t)
	empty := t.TempDir()
	t.Setenv("HOME", empty)
	t.Setenv("XDG_CONFIG_HOME", empty)

	cfg, err := Load(writeTempConfig(t, `
repositories:
  offsite: {backend: local, url: /srv/repo, password: !locked "`+locked+`"}
`+lockedConfigRest))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res := Validate(cfg); len(res.Errors) != 0 {
		t.Fatalf("validation failed on a machine with no host key: %v", res.Errors)
	}
}
