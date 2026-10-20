package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// sourceConfig is a one-job config whose source is the given flow mapping.
func sourceConfig(source string) string {
	return `policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/nas, password: x}
backups:
  docs:
    source: ` + source + `
    policy: hot
    repositories: [nas]
`
}

func TestSource_ExclusionsAccepted(t *testing.T) {
	setHome(t)
	cfg, problems := Check(writeTempConfig(t, sourceConfig(`{paths: [/home/me/.local], exclude: [/home/me/.local/share/Trash, node_modules], exclude_caches: true, one_file_system: true}`)))
	if cfg == nil || len(problems) != 0 {
		t.Fatalf("got %+v", problems)
	}
	src := cfg.Backups["docs"].Source
	if strings.Join(src.Exclude, "|") != "/home/me/.local/share/Trash|node_modules" || !src.ExcludeCaches || !src.OneFileSystem {
		t.Fatalf("got source %+v", src)
	}
}

func TestSource_ExclusionsDefaultOff(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, sourceConfig(`{paths: [/a]}`)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if src := cfg.Backups["docs"].Source; src.Exclude != nil || src.ExcludeCaches || src.OneFileSystem {
		t.Fatalf("got source %+v", src)
	}
}

func TestSource_ExclusionsWithoutPathsRejected(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, sourceConfig(`{exclude: [Cache]}`)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if errs := Validate(cfg).Errors; !containsJobError(errs, "docs", "source.paths must be non-empty") {
		t.Fatalf("expected the empty-paths error, got %v", errs)
	}
}

func TestSource_OtherTypesStillRejected(t *testing.T) {
	for source, want := range map[string]string{
		`{paths: [/a], container: x}`: "unsupported source type(s) [container]",
		`{paths: [/a], exlude: [x]}`:  `did you mean "exclude"?`,
	} {
		_, err := Load(writeTempConfig(t, sourceConfig(source)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want an error saying %q", source, err, want)
		}
	}
}

func TestSource_WrongTypesLocated(t *testing.T) {
	for source, want := range map[string]string{
		`{paths: [/a], exclude: Trash}`:          "source.exclude: must be a list of patterns",
		`{paths: [/a], exclude: [{x: y}]}`:       "source.exclude: must be a list of patterns",
		`{paths: [/a], exclude_caches: "yes"}`:   "source.exclude_caches: must be true or false",
		`{paths: [/a], exclude_caches: yes}`:     "source.exclude_caches: must be true or false",
		`{paths: [/a], one_file_system: [true]}`: "source.one_file_system: must be true or false",
	} {
		content := sourceConfig(source)
		cfg, problems := Check(writeTempConfig(t, content))
		if cfg != nil || len(problems) != 1 {
			t.Errorf("%s: got %+v", source, problems)
			continue
		}
		line, _ := at(t, content, "source:")
		if p := problems[0]; p.Severity != SeverityError || p.Line != line || p.Message != want {
			t.Errorf("%s: got %+v, want %q on line %d", source, p, want, line)
		}
	}
}

func TestLoad_ExpandsLeadingTildeInExcludes(t *testing.T) {
	home := setHome(t)
	cfg, err := Load(writeTempConfig(t, sourceConfig(`{paths: [~], exclude: ["~/.local/share/Trash", "~", "~$*", "~alice/x", "/srv/~backup", "$HOME/x"]}`)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{filepath.Join(home, ".local", "share", "Trash"), home, "~$*", "~alice/x", "/srv/~backup", "$HOME/x"}
	if got := cfg.Backups["docs"].Source.Exclude; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLoad_ExcludeNeedsAHomeDirectory(t *testing.T) {
	orig := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userHomeDir = orig })

	_, err := Load(writeTempConfig(t, sourceConfig(`{paths: [/a], exclude: ["~/.cache"]}`)))
	if err == nil || !strings.Contains(err.Error(), `job "docs"`) || !strings.Contains(err.Error(), `"~/.cache"`) {
		t.Fatalf("expected an error naming the job and the pattern, got %v", err)
	}
}

func TestExcludes_Checks(t *testing.T) {
	cases := map[string]struct {
		paths, exclude string
		severity       string // "" for no problem
		says           string
	}{
		"bad glob syntax":       {`[/data]`, `["/data/[abc"]`, SeverityError, `"/data/[abc" is not a valid pattern`},
		"bad bare pattern":      {`[/data]`, `["[abc"]`, SeverityError, "is not a valid pattern"},
		"empty pattern":         {`[/data]`, `[""]`, SeverityError, "must not be empty"},
		"outside every source":  {`[/home/me/documents]`, `[/home/me/.cache]`, SeverityWarning, "outside every source path"},
		"covers source paths":   {`[/home/me/.ssh, /home/me/.config]`, `[/home/me]`, SeverityWarning, `excludes all of "/home/me/.ssh" and "/home/me/.config"`},
		"equals a source path":  {`[/srv/a, /srv/b]`, `[/srv/b/]`, SeverityWarning, `excludes all of "/srv/b"`},
		"wildcard over source":  {`[/home/me]`, `["/home/*"]`, SeverityWarning, `excludes all of "/home/me"`},
		"under a source path":   {`[/home/me]`, `[/home/me/.local/share/Trash]`, "", ""},
		"wildcard under source": {`[/home/me]`, `["/home/*/.cache"]`, "", ""},
		"double star":           {`[/home/me/documents]`, `["/home/**/x"]`, "", ""},
		"relative not checked":  {`[/home/me/documents]`, `[Trash, "*.iso", "**/node_modules"]`, "", ""},
		"relative source path":  {`[/home/me, data]`, `[/srv/x]`, "", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			content := sourceConfig(`{paths: ` + c.paths + `, exclude: ` + c.exclude + `}`)
			cfg, problems := Check(writeTempConfig(t, content))
			if cfg == nil {
				t.Fatalf("not parsed: %+v", problems)
			}
			var got []Problem
			for _, p := range problems {
				if strings.HasPrefix(p.Path, "backups.docs.source.exclude") {
					got = append(got, p)
				}
			}
			if c.severity == "" {
				if len(got) != 0 {
					t.Fatalf("expected no problem, got %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("expected one problem, got %+v", problems)
			}
			p := got[0]
			line, _ := at(t, content, "exclude:")
			if p.Severity != c.severity || p.Job != "docs" || p.Path != "backups.docs.source.exclude.0" || p.Line != line || !strings.Contains(p.Message, c.says) {
				t.Fatalf("got %+v, want a %s on line %d saying %q", p, c.severity, line, c.says)
			}
			if valid := !HasErrors(problems); valid != (c.severity == SeverityWarning) {
				t.Fatalf("valid = %v with %+v", valid, problems)
			}
		})
	}
}

func TestExcludes_LocatedAtTheirListItem(t *testing.T) {
	content := `policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/nas, password: x}
backups:
  docs:
    source:
      paths: [/home/me/documents]
      exclude:
        - Trash
        - /elsewhere
    policy: hot
    repositories: [nas]
`
	_, problems := Check(writeTempConfig(t, content))
	p := problemAt(t, problems, "backups.docs.source.exclude.1")
	if line, col := at(t, content, "/elsewhere"); p.Line != line || p.Column != col {
		t.Errorf("got %d:%d, want %d:%d", p.Line, p.Column, line, col)
	}
}
