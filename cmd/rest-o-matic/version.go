package main

import (
	"fmt"
	"runtime/debug"
)

// buildInfo is everything about the running binary's provenance that
// version reporting needs, pulled from Go's own automatic VCS/module
// stamping (runtime/debug.ReadBuildInfo) - no hardcoded value, no
// -ldflags injection required.
type buildInfo struct {
	Version    string
	Commit     string
	CommitTime string
	Dirty      bool
	GoVersion  string
}

// readBuildInfo reads the running binary's embedded build info. If build
// info or any individual VCS setting is unavailable (e.g. the binary was
// built from a source tree with no .git directory), the corresponding
// field is left at its zero value rather than causing a failure.
func readBuildInfo() buildInfo {
	bi := buildInfo{Version: "(unknown)"}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return bi
	}

	bi.Version = info.Main.Version
	bi.GoVersion = info.GoVersion

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			bi.Commit = s.Value
		case "vcs.time":
			bi.CommitTime = s.Value
		case "vcs.modified":
			bi.Dirty = s.Value == "true"
		}
	}

	return bi
}

// shortLine is the single-line version string shown on a no-argument
// invocation.
func (b buildInfo) shortLine() string {
	return fmt.Sprintf("rest-o-matic %s", b.Version)
}

// detailedBlock is the multi-line build-info block shown by --version.
func (b buildInfo) detailedBlock() string {
	commit := orUnknown(b.Commit)
	commitTime := orUnknown(b.CommitTime)
	goVersion := orUnknown(b.GoVersion)

	return fmt.Sprintf(
		"rest-o-matic %s\n  commit:      %s\n  commit time: %s\n  dirty:       %t\n  go version:  %s",
		b.Version, commit, commitTime, b.Dirty, goVersion,
	)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
