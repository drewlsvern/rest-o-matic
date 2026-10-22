package config

import (
	"strings"
	"testing"
	"time"
)

func TestProgressInterval(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"":                              DefaultProgressInterval,
		"progress_interval: 1m\n":       time.Minute,
		"progress_interval: 90s\n":      90 * time.Second,
		"progress_interval: 2h\n":       2 * time.Hour,
		"progress_interval: 10s\n":      10 * time.Second,
		"progress_interval: 0\n":        0,
		`progress_interval: "0"` + "\n": 0,
	} {
		cfg, problems := Check(writeTempConfig(t, value+sourceConfig(`{paths: [/a]}`)))
		if cfg == nil || len(problems) != 0 {
			t.Errorf("%q: %+v", value, problems)
			continue
		}
		if got := cfg.ProgressLineInterval(); got != want {
			t.Errorf("%q: got %v, want %v", value, got, want)
		}
	}
}

func TestProgressInterval_Rejected(t *testing.T) {
	for value, says := range map[string]string{
		"often": `"often" is not a duration like 30s, 5m or 1h, or 0`,
		"5":     `"5" is not a duration`,
		"1.5m":  `"1.5m" is not a duration`,
		"2s":    "must be at least 10s, or 0",
		"[5m]":  "must be a duration like 30s, 5m or 1h, or 0",
	} {
		content := "progress_interval: " + value + "\n" + sourceConfig(`{paths: [/a]}`)
		cfg, problems := Check(writeTempConfig(t, content))
		if cfg != nil || len(problems) != 1 || problems[0].Line != 1 || !strings.Contains(problems[0].Message, "progress_interval") || !strings.Contains(problems[0].Message, says) {
			t.Errorf("%s: got %+v, want one error on line 1 saying %q", value, problems, says)
		}
	}
}

// connectConfig is a config with repositories nas and s3; top and the
// repositories' extra keys are inserted as given.
func connectConfig(top, nas, s3 string) string {
	return top + `policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/nas, password: x` + nas + `}
  s3: {backend: s3, url: "s3:s3.example.com/b", password: x` + s3 + `}
backups:
  docs: {source: {paths: [/a]}, policy: hot, repositories: [nas, s3]}
`
}

func TestConnectTimeout(t *testing.T) {
	for name, c := range map[string]struct {
		top, nas, s3 string
		wantNas      time.Duration
		wantS3       time.Duration
	}{
		"default":              {"", "", "", DefaultConnectTimeout, DefaultConnectTimeout},
		"top-level default":    {"connect_timeout: 30s\n", "", "", 30 * time.Second, 30 * time.Second},
		"repository overrides": {"connect_timeout: 30s\n", ", connect_timeout: 5m", "", 5 * time.Minute, 30 * time.Second},
		"turned off":           {"", ", connect_timeout: 0", "", 0, DefaultConnectTimeout},
	} {
		cfg, problems := Check(writeTempConfig(t, connectConfig(c.top, c.nas, c.s3)))
		if cfg == nil || len(problems) != 0 {
			t.Errorf("%s: %+v", name, problems)
			continue
		}
		if got := cfg.ConnectTimeoutFor("nas"); got != c.wantNas {
			t.Errorf("%s: nas got %v, want %v", name, got, c.wantNas)
		}
		if got := cfg.ConnectTimeoutFor("s3"); got != c.wantS3 {
			t.Errorf("%s: s3 got %v, want %v", name, got, c.wantS3)
		}
	}
}

func TestConnectTimeout_Rejected(t *testing.T) {
	for value, says := range map[string]string{
		"soon": `connect_timeout: "soon" is not a duration like 30s, 5m or 1h, or 0`,
		"5s":   "connect_timeout: must be at least 10s, or 0 to skip the check",
	} {
		content := connectConfig("", "", ", connect_timeout: "+value)
		cfg, problems := Check(writeTempConfig(t, content))
		line, _ := at(t, content, "  s3:")
		if cfg != nil || len(problems) != 1 || problems[0].Line != line || !strings.Contains(problems[0].Message, says) {
			t.Errorf("%s: got %+v, want one error on line %d saying %q", value, problems, line, says)
		}
	}
	// progress_interval's messages still name progress_interval.
	_, problems := Check(writeTempConfig(t, "progress_interval: 5s\n"+sourceConfig(`{paths: [/a]}`)))
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "progress_interval: must be at least 10s, or 0 to turn timed progress lines off") {
		t.Errorf("got %+v", problems)
	}
}
