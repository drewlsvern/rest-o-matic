package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drewlsvern/rest-o-matic/internal/secrets"
)

// remoteSchemes are the restic backends addressed with a "<scheme>:" url
// prefix. A url starting with one of these prefixes "has a known scheme".
var remoteSchemes = []string{"s3", "sftp", "rest", "swift", "b2", "azure", "gs", "rclone"}

// backendScheme maps each recognised `backend` value to the url scheme it
// requires. "local" requires none; "rest-server" is an alias for "rest".
var backendScheme = func() map[string]string {
	m := map[string]string{"local": "", "rest-server": "rest"}
	for _, s := range remoteSchemes {
		m[s] = s
	}
	return m
}()

// urlScheme returns the known scheme url begins with, or "" if none.
func urlScheme(url string) string {
	for _, s := range remoteSchemes {
		if strings.HasPrefix(url, s+":") {
			return s
		}
	}
	return ""
}

// CheckRepository checks a repository's backend against the scheme of its
// url. Definite contradictions are errors; configurations that are probably
// but not certainly wrong are warnings. The url itself is never modified.
func CheckRepository(name string, repo Repository) (errs []error, warns []Warning) {
	at := "repositories." + name + "."
	fail := func(field, format string, args ...any) {
		errs = append(errs, ValidationError{Repository: name, Path: at + field, Message: fmt.Sprintf(format, args...)})
	}

	// A locked value is only checked for its form. No key is read, so the
	// result is the same on every machine.
	if repo.Password.Locked && secrets.WellFormed(repo.Password.Value) != nil {
		fail("password", "password is marked %s but is not a valid locked value; create one with `rest-o-matic secret lock`", secrets.Tag)
	}
	envNames := make([]string, 0, len(repo.Env))
	for envName := range repo.Env {
		envNames = append(envNames, envName)
	}
	sort.Strings(envNames)
	for _, envName := range envNames {
		if v := repo.Env[envName]; v.Locked && secrets.WellFormed(v.Value) != nil {
			fail("env."+envName, "env %s is marked %s but is not a valid locked value; create one with `rest-o-matic secret lock`", envName, secrets.Tag)
		}
	}
	warn := func(field, format string, args ...any) {
		warns = append(warns, Warning{Repository: name, Path: at + field, Message: fmt.Sprintf(format, args...)})
	}

	if repo.Backend == "" {
		fail("backend", "backend is required (e.g. local, s3, sftp, rest, b2)")
		return
	}
	want, known := backendScheme[repo.Backend]
	if !known {
		warn("backend", "unrecognised backend %q; url is passed to restic unchanged and not checked", repo.Backend)
		return
	}

	got := urlScheme(repo.URL)
	switch {
	case got != "" && want == "":
		fail("url", "backend %q but url begins with %q; set backend: %s or use a filesystem path", repo.Backend, got+":", got)
	case got != "" && got != want:
		fail("url", "backend %q requires a url beginning with %q, but url begins with %q", repo.Backend, want+":", got+":")
	case got == "" && want != "":
		fail("url", "backend %q requires a url beginning with %q; without it restic treats the url as a local directory", repo.Backend, want+":")
	case got == "" && !filepath.IsAbs(repo.URL):
		warn("url", "local path %q is relative and resolves against the current working directory; use an absolute path", repo.URL)
	}
	return
}
