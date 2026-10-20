package config

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// checkExcludes checks a job's exclude patterns from the config alone: a
// pattern restic would refuse (an error, since restic only refuses it once
// the before hooks have run), and an anchored pattern that either can't
// match anything the job backs up or takes out a whole source path
// (warnings).
//
// restic matches a pattern one path component at a time, each by
// filepath.Match's rules, with `**` standing for any number of components;
// a pattern that isn't anchored at the root matches at any depth. The
// comparison here follows that.
func checkExcludes(job string, src Source) (errs []error, warns []Warning) {
	for i, p := range src.Exclude {
		at := fmt.Sprintf("backups.%s.source.exclude.%d", job, i)
		if p == "" {
			errs = append(errs, ValidationError{Job: job, Path: at, Message: "exclude pattern must not be empty"})
			continue
		}
		parts := patternParts(p)
		if !validPattern(parts) {
			errs = append(errs, ValidationError{Job: job, Path: at, Message: fmt.Sprintf("exclude pattern %q is not a valid pattern", p)})
			continue
		}
		if !anchored(p) {
			continue
		}

		var covered []string
		outside, unsure := true, false
		for _, s := range src.Paths {
			if !anchored(s) {
				unsure = true
				continue
			}
			switch comparePattern(parts, patternParts(s)) {
			case patternCovers:
				covered = append(covered, fmt.Sprintf("%q", s))
				outside = false
			case patternUnder:
				outside = false
			case patternUnsure:
				unsure = true
			}
		}
		switch {
		case len(covered) > 0:
			warns = append(warns, Warning{Job: job, Path: at, Message: fmt.Sprintf("exclude pattern %q excludes all of %s, so nothing from there is backed up", p, strings.Join(covered, " and "))})
		case outside && !unsure && len(src.Paths) > 0:
			warns = append(warns, Warning{Job: job, Path: at, Message: fmt.Sprintf("exclude pattern %q is outside every source path of this job, so it excludes nothing", p)})
		}
	}
	return errs, warns
}

// anchored reports whether a pattern or path starts from the root.
func anchored(p string) bool {
	return strings.HasPrefix(filepath.ToSlash(p), "/") || filepath.IsAbs(p)
}

// patternParts splits a pattern or path into its components.
func patternParts(p string) []string {
	p = path.Clean(filepath.ToSlash(p))
	var parts []string
	for _, c := range strings.Split(p, "/") {
		if c != "" {
			parts = append(parts, c)
		}
	}
	return parts
}

// validPattern reports whether every component is one restic accepts.
func validPattern(parts []string) bool {
	for _, c := range parts {
		if c == "**" {
			continue
		}
		if _, err := filepath.Match(c, ""); err != nil {
			return false
		}
	}
	return true
}

type patternRelation int

const (
	patternOutside patternRelation = iota // can match nothing under the path
	patternUnder                          // matches only things under the path
	patternCovers                         // matches the path or a directory holding it
	patternUnsure                         // a ** was reached before it was settled
)

// comparePattern relates an anchored pattern to a source path, both split
// into components, by matching them from the root for as far as the
// shorter of the two goes.
func comparePattern(pattern, src []string) patternRelation {
	for i := 0; i < len(pattern) && i < len(src); i++ {
		if pattern[i] == "**" {
			return patternUnsure
		}
		if ok, _ := filepath.Match(pattern[i], src[i]); !ok {
			return patternOutside
		}
	}
	if len(pattern) <= len(src) {
		return patternCovers
	}
	return patternUnder
}
