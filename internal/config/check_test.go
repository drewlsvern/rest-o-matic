package config

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// at returns the 1-based line and character column of the first match of
// marker in content, for checking where a problem was placed.
func at(t *testing.T, content, marker string) (int, int) {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		if j := strings.Index(line, marker); j >= 0 {
			return i + 1, utf8.RuneCountInString(line[:j]) + 1
		}
	}
	t.Fatalf("%q is not in the config", marker)
	return 0, 0
}

// problemAt returns the one problem with the given path.
func problemAt(t *testing.T, problems []Problem, path string) Problem {
	t.Helper()
	for _, p := range problems {
		if p.Path == path {
			return p
		}
	}
	t.Fatalf("no problem with path %s in %+v", path, problems)
	return Problem{}
}

func TestCheck_Locations(t *testing.T) {
	content := `x-shared:
  url: &u "s3:https://s3.example.com/b"
  s3: &s3
    backend: local
    url: "s3:https://s3.example.com/c"
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas:
    backend: local
    url: *u
    password: x
  offsite:
    <<: *s3
    password: y
backups:
  docs:
    source: {paths: ["/é/a"]}
    policy: hott
    repositories: [nas, nowhere]
  media:
    source: {paths: ["/m"]}
    repositories: [nas]
`
	cfg, problems := Check(writeTempConfig(t, content))
	if cfg == nil || !HasErrors(problems) {
		t.Fatalf("got %v, %+v", cfg, problems)
	}

	p := problemAt(t, problems, "backups.docs.policy")
	if line, col := at(t, content, "hott"); p.Line != line || p.Column != col || p.Job != "docs" || p.Severity != SeverityError {
		t.Errorf("undefined policy: got %+v, want line %d column %d", p, line, col)
	}
	p = problemAt(t, problems, "backups.media.policy")
	if line, col := at(t, content, "media:"); p.Line != line || p.Column != col {
		t.Errorf("missing policy: got line %d column %d, want the media key at %d:%d", p.Line, p.Column, line, col)
	}
	p = problemAt(t, problems, "backups.docs.repositories.1")
	if line, col := at(t, content, "nowhere"); p.Line != line || p.Column != col {
		t.Errorf("undefined repository: got %d:%d, want %d:%d", p.Line, p.Column, line, col)
	}
	p = problemAt(t, problems, "repositories.nas.url")
	if line, col := at(t, content, "*u"); p.Line != line || p.Column != col || p.Repository != "nas" {
		t.Errorf("url through an alias: got %+v, want %d:%d", p, line, col)
	}
	p = problemAt(t, problems, "repositories.offsite.url")
	if line, col := at(t, content, "*s3"); p.Line != line || p.Column != col {
		t.Errorf("url through a merge key: got %d:%d, want %d:%d", p.Line, p.Column, line, col)
	}

	for i := 1; i < len(problems); i++ {
		if problems[i].Line < problems[i-1].Line {
			t.Fatalf("problems are not sorted by line: %+v", problems)
		}
	}
	if got := problemAt(t, problems, "backups.docs.policy").String(); !strings.HasPrefix(got, "line 19: job \"docs\": references undefined policy") {
		t.Errorf("text: %s", got)
	}
}

// Columns count characters: a multi-byte character earlier on the line
// moves the column by one.
func TestCheck_ColumnsCountCharacters(t *testing.T) {
	content := `policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/nas, password: x}
backups:
  docs: {source: {paths: ["/éé"]}, policy: hott, repositories: [nas]}
`
	_, problems := Check(writeTempConfig(t, content))
	p := problemAt(t, problems, "backups.docs.policy")
	if line, col := at(t, content, "hott"); p.Line != line || p.Column != col {
		t.Errorf("got %d:%d, want %d:%d", p.Line, p.Column, line, col)
	}
}

func TestCheck_ParseProblems(t *testing.T) {
	cases := map[string]struct {
		content string
		lines   []int
		text    string
	}{
		// The line where the unclosed { starts.
		"syntax error from the parser": {"policies:\n  hot: {schedule: hourly\nrepositories: [\n", []int{2}, "did not find expected"},
		// Unclosed on line 1: yaml.v3 then gives the line it noticed it on.
		"syntax error on the first line":  {"a: [1, 2\nb: 1\n", []int{2}, "did not find expected"},
		"syntax error from the scanner":   {"a: 1\n  b: 2\n", []int{2}, "mapping values are not allowed"},
		"stray line":                      {"- a\nb: 1\n", []int{2}, "did not find expected '-' indicator"},
		"type errors":                     {"max_concurrent: lots\npolicies:\n  hot: {schedule: hourly, retention: {hourly: many}}\n", []int{1, 3}, "cannot unmarshal"},
		"locked value in the wrong place": {"repositories:\n  nas: {backend: local, url: !locked \"x\"}\n", []int{2}, "!locked is only allowed"},
		"unknown notify key":              {"notify:\n  failure: [echo]\n  sometimes: [echo]\n", []int{3}, "unknown key"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, problems := Check(writeTempConfig(t, c.content))
			if cfg != nil || len(problems) != len(c.lines) {
				t.Fatalf("got %v, %+v", cfg, problems)
			}
			for i, p := range problems {
				if p.Severity != SeverityError || p.Line != c.lines[i] || strings.HasPrefix(p.Message, "line ") || strings.HasPrefix(p.Message, "yaml:") {
					t.Errorf("problem %d: %+v, want line %d", i, p, c.lines[i])
				}
				if !strings.Contains(p.Message, c.text) {
					t.Errorf("problem %d: %q does not say %q", i, p.Message, c.text)
				}
			}
		})
	}
}

func TestCheck_UnreadableFile(t *testing.T) {
	cfg, problems := Check(filepath.Join(t.TempDir(), "missing.yaml"))
	if cfg != nil || len(problems) != 1 || problems[0].Line != 0 || !strings.Contains(problems[0].Message, "reading config") {
		t.Fatalf("got %v, %+v", cfg, problems)
	}
}

func TestCheck_ValidWithWarning(t *testing.T) {
	cfg, problems := Check(writeTempConfig(t, `policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: relative/repo, password: x}
backups:
  docs: {source: {paths: ["/a"]}, policy: hot, repositories: [nas]}
`))
	if cfg == nil || HasErrors(problems) || len(problems) != 1 || problems[0].Severity != SeverityWarning || problems[0].Line != 4 {
		t.Fatalf("got %+v", problems)
	}
}
