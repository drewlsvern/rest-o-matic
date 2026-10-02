package config

import (
	"errors"
	"strings"
	"testing"
)

// fakePlatform makes checkReadMode see the given OS, and podman as present
// or not, restoring the real values when the test ends.
func fakePlatform(t *testing.T, os string, havePodman bool) {
	t.Helper()
	origGOOS, origLookPath := goos, lookPath
	t.Cleanup(func() { goos, lookPath = origGOOS, origLookPath })
	goos = os
	lookPath = func(string) (string, error) {
		if havePodman {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	}
}

func readModeConfig(t *testing.T, readAs string) *Config {
	t.Helper()
	line := ""
	if readAs != "" {
		line = "    read_as: " + readAs + "\n"
	}
	cfg, err := Load(writeTempConfig(t, `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /tmp/nas}
backups:
  gitea:
    source: {paths: ["/srv/gitea"]}
    policy: hot
    repositories: [nas]
`+line))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestReadMode_OmittedIsDirect(t *testing.T) {
	fakePlatform(t, "linux", true)
	cfg := readModeConfig(t, "")
	if got := cfg.Backups["gitea"].ReadMode(); got != ReadDirect {
		t.Fatalf("ReadMode() = %q, want %q", got, ReadDirect)
	}
	if res := Validate(cfg); len(res.Errors) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("expected no errors or warnings, got %+v", res)
	}
}

func TestReadMode_PodmanUnshareAcceptedOnLinux(t *testing.T) {
	fakePlatform(t, "linux", true)
	cfg := readModeConfig(t, "podman-unshare")
	if got := cfg.Backups["gitea"].ReadMode(); got != ReadPodmanUnshare {
		t.Fatalf("ReadMode() = %q, want %q", got, ReadPodmanUnshare)
	}
	if res := Validate(cfg); len(res.Errors) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("expected no errors or warnings, got %+v", res)
	}
}

func TestReadMode_UnknownValueRejected(t *testing.T) {
	fakePlatform(t, "linux", true)
	res := Validate(readModeConfig(t, "sudo"))
	if len(res.Errors) != 1 {
		t.Fatalf("expected one error, got %+v", res.Errors)
	}
	msg := res.Errors[0].Error()
	for _, want := range []string{`job "gitea"`, `"sudo"`, "direct", "podman-unshare"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to mention %s, got %q", want, msg)
		}
	}
}

func TestReadMode_PodmanUnshareRejectedOffLinux(t *testing.T) {
	for _, os := range []string{"windows", "darwin"} {
		t.Run(os, func(t *testing.T) {
			fakePlatform(t, os, true)
			res := Validate(readModeConfig(t, "podman-unshare"))
			if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "not needed on "+os) {
				t.Fatalf("expected a not-needed error for %s, got %+v", os, res.Errors)
			}
		})
	}
}

func TestReadMode_MissingPodmanWarnsOnLinux(t *testing.T) {
	fakePlatform(t, "linux", false)
	res := Validate(readModeConfig(t, "podman-unshare"))
	if len(res.Errors) != 0 {
		t.Fatalf("expected no errors, got %+v", res.Errors)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Job != "gitea" || !strings.Contains(res.Warnings[0].String(), `job "gitea"`) {
		t.Fatalf("expected one warning for job gitea, got %+v", res.Warnings)
	}
}
