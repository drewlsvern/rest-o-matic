package config

import (
	"strings"
	"testing"
)

func warningsOf(problems []Problem) []Problem {
	var out []Problem
	for _, p := range problems {
		if p.Severity == SeverityWarning {
			out = append(out, p)
		}
	}
	return out
}

func TestUnknownKeys(t *testing.T) {
	content := `x-shared:
  base: &base
    hok: {before: [echo merged]}
  anything: {goes: here}
policies:
  hot: {schedule: hourly, retension: {hourly: 24}}
repositories:
  nas:
    backend: local
    url: /srv/nas
    pasword_file: /x
    password: y
    env: {MY_OWN_VARIABLE: x, ANOTHER: y}
backups:
  docs:
    source: {paths: [/a]}
    policy: hot
    repositories: [nas]
    hook: {before: [echo hi]}
  media:
    <<: *base
    source: {paths: [/m]}
    policy: hot
    repositories: [{repo: nas, retention: {dayly: 3}}]
    nohooks: {before: [echo disabled]}
  more:
    <<: *base
    source: {paths: [/n]}
    policy: hot
    repositories: [nas]
colour: blue
`
	cfg, problems := Check(writeTempConfig(t, content))
	if cfg == nil || HasErrors(problems) {
		t.Fatalf("unknown keys made the config invalid: %+v", problems)
	}
	type want struct{ path, suggestion, marker, job, repo string }
	wants := []want{
		{"policies.hot.retension", "retention", "retension", "", ""},
		{"repositories.nas.pasword_file", "password_file", "pasword_file", "", "nas"},
		{"backups.docs.hook", "hooks", "hook:", "docs", ""},
		{"backups.media.hok", "hooks", "hok:", "media", ""},
		{"backups.media.repositories.0.retention.dayly", "daily", "dayly", "media", ""},
		{"backups.media.nohooks", "hooks", "nohooks", "media", ""},
		{"colour", "", "colour", "", ""},
	}
	warnings := warningsOf(problems)
	if len(warnings) != len(wants) {
		t.Fatalf("got %d warnings, want %d (the merged hok once, nothing for x- keys or env names):\n%+v", len(warnings), len(wants), warnings)
	}
	for _, w := range wants {
		p := problemAt(t, warnings, w.path)
		line, col := at(t, content, w.marker)
		if p.Line != line || p.Column != col || p.Job != w.job || p.Repository != w.repo {
			t.Errorf("%s: got %+v, want %d:%d", w.path, p, line, col)
		}
		if w.suggestion != "" && !strings.Contains(p.Message, `did you mean "`+w.suggestion+`"?`) {
			t.Errorf("%s: %q does not suggest %s", w.path, p.Message, w.suggestion)
		}
		if w.suggestion == "" && strings.Contains(p.Message, "did you mean") {
			t.Errorf("%s: %q suggests something", w.path, p.Message)
		}
	}

	// Ignored, not corrected: neither the misspelt nor the disabled hooks
	// become the job's hooks.
	for _, job := range []string{"docs", "media", "more"} {
		if hooks := cfg.Backups[job].Hooks; len(hooks.Before) != 0 {
			t.Errorf("job %s got hooks %v from an unknown key", job, hooks.Before)
		}
	}
	if cfg.Repositories["nas"].PasswordFile != "" {
		t.Error("pasword_file was read as password_file")
	}
}

func TestUnknownKeys_DecodersSuggestToo(t *testing.T) {
	for name, c := range map[string]struct{ content, want string }{
		"source":         {"backups:\n  docs:\n    source:\n      path: [/a]\n", `line 4: source: unsupported source type(s) [path]; only 'paths' is supported in this version; did you mean "paths"?`},
		"notify":         {"notify:\n  failur: [echo]\n", `line 2: notify: unknown key "failur" (want failure, recovery, or success); did you mean "failure"?`},
		"after outcomes": {"backups:\n  docs:\n    hooks:\n      after: {sucess: [echo]}\n", `did you mean "success"?`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, c.content))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want it to contain %s", err, c.want)
			}
		})
	}
}

// The walk interprets only part of JSON Schema. If the schema starts using
// a keyword it doesn't know, it could miss or wrongly report keys.
func TestUnknownKeys_WalkUnderstandsTheSchema(t *testing.T) {
	var visit func(s map[string]any, where string)
	visit = func(s map[string]any, where string) {
		for k, v := range s {
			if !schemaWalkKeywords[k] {
				t.Errorf("%s: the schema uses %q, which the unknown-key walk doesn't handle", where, k)
			}
			switch k {
			case "properties", "patternProperties", "definitions":
				for name, sub := range v.(map[string]any) {
					visit(sub.(map[string]any), where+"/"+k+"/"+name)
				}
			case "additionalProperties", "items":
				if sub, ok := v.(map[string]any); ok {
					visit(sub, where+"/"+k)
				}
			case "oneOf", "anyOf":
				for i, sub := range v.([]any) {
					visit(sub.(map[string]any), where+"/"+k+"/"+string(rune('0'+i)))
				}
			}
		}
	}
	visit(parsedSchema(), "#")
}

func TestSuggest(t *testing.T) {
	// Within two edits only: a plural three edits away is not suggested.
	for key, want := range map[string]string{"hook": "hooks", "polcy": "policy", "zzz": "", "tag": "tags", "repository": ""} {
		if got := suggest(key, []string{"hooks", "policy", "tags", "repositories", "retention"}); got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
}
