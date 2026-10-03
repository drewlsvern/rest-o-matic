package config

import (
	"strings"
	"testing"
)

func TestLoad_PlainMarkerOnEnvValue(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, `
x-shared:
  prefix: &prefix !plain "host-a/"
repositories:
  offsite:
    backend: local
    url: /srv/repo
    password: x
    env:
      MY_BUCKET_PREFIX: !plain "host-a/"
      SHARED_PREFIX: *prefix
      UNMARKED: host-a/
`+lockedConfigRest))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	env := cfg.Repositories["offsite"].Env
	for _, name := range []string{"MY_BUCKET_PREFIX", "SHARED_PREFIX"} {
		if got := env[name]; got.Value != "host-a/" || got.Locked || !got.MarkedPlain {
			t.Errorf("%s: got %+v, want plain text marked as not secret", name, got)
		}
	}
	if got := env["UNMARKED"]; got != Plain("host-a/") {
		t.Errorf("UNMARKED: got %+v, want plain and unmarked", got)
	}
	if res := Validate(cfg); len(res.Errors) != 0 {
		t.Errorf("validation failed: %v", res.Errors)
	}
}

func TestLoad_PlainMarkerElsewhereIsRejected(t *testing.T) {
	cases := map[string]struct{ config, where string }{
		"password": {`
repositories:
  offsite:
    backend: local
    url: /srv/repo
    password: !plain "x"
` + lockedConfigRest, "repositories.offsite.password"},
		"url": {`
repositories:
  offsite:
    backend: local
    url: !plain /srv/repo
    password: x
` + lockedConfigRest, "repositories.offsite.url"},
		"hook through an alias": {`
x-shared:
  cmd: &cmd !plain "echo hi"
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
      before: [*cmd]
`, "backups.documents.hooks.before.0"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, c.config))
			if err == nil {
				t.Fatal("expected the config to be rejected")
			}
			if !strings.Contains(err.Error(), PlainTag) || !strings.Contains(err.Error(), c.where) || !strings.Contains(err.Error(), "line ") {
				t.Fatalf("got %v, want an error naming %s, %s and its line", err, PlainTag, c.where)
			}
		})
	}
}
