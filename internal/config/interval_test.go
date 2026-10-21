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
