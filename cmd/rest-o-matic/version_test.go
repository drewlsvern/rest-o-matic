package main

import (
	"strings"
	"testing"
)

// go test builds a synthetic test binary that Go does not VCS-stamp at all
// (confirmed: no vcs.* build settings are present, unlike a plain `go build`
// of the actual package) - so this doubles as the "VCS settings unavailable"
// case: readBuildInfo must degrade gracefully rather than panic or assume
// they're present.
func TestReadBuildInfo_DegradesGracefullyWithoutVCSInfo(t *testing.T) {
	bi := readBuildInfo()
	if bi.Version == "" {
		t.Fatal("expected a non-empty Version even without VCS info")
	}
	if bi.GoVersion == "" {
		t.Error("expected a non-empty GoVersion")
	}
	// Commit/CommitTime are expected to be empty here specifically because
	// go test's build has no VCS settings - this is the missing-fields case,
	// not a bug. detailedBlock renders these gracefully (see the test below).
}

func TestBuildInfo_ShortLineContainsVersion(t *testing.T) {
	bi := buildInfo{Version: "v1.2.3"}
	line := bi.shortLine()
	if !strings.Contains(line, "v1.2.3") {
		t.Fatalf("expected short line to contain the version, got %q", line)
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("expected short line to be a single line, got %q", line)
	}
}

func TestBuildInfo_DetailedBlockContainsAllFields(t *testing.T) {
	bi := buildInfo{
		Version:    "v1.2.3",
		Commit:     "abc123",
		CommitTime: "2026-01-01T00:00:00Z",
		Dirty:      true,
		GoVersion:  "go1.25.0",
	}
	block := bi.detailedBlock()
	for _, want := range []string{"v1.2.3", "abc123", "2026-01-01T00:00:00Z", "true", "go1.25.0"} {
		if !strings.Contains(block, want) {
			t.Errorf("expected detailed block to contain %q, got:\n%s", want, block)
		}
	}
}

func TestBuildInfo_DetailedBlockHandlesMissingFields(t *testing.T) {
	bi := buildInfo{Version: "(unknown)"}
	block := bi.detailedBlock()
	if !strings.Contains(block, "unknown") {
		t.Fatalf("expected missing commit/commit-time/go-version to render as 'unknown', got:\n%s", block)
	}
	if !strings.Contains(block, "dirty:       false") {
		t.Fatalf("expected dirty to default to false when unset, got:\n%s", block)
	}
}
