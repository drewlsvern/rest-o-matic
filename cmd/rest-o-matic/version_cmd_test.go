package main

import "testing"

func TestCLI_NoArgs_ShowsShortVersionLineAndCommandListing(t *testing.T) {
	bin := buildBinary(t)
	stdout, _, code := runCLI(t, bin, t.TempDir())
	// cobra's no-args behavior with no RunE on the root command still
	// exits 0 and prints help.
	if code != 0 {
		t.Fatalf("expected exit 0 for no-args invocation, got %d", code)
	}
	if !contains(stdout, "rest-o-matic v") {
		t.Fatalf("expected a version line, got:\n%s", stdout)
	}
	if !contains(stdout, "Available Commands:") || !contains(stdout, "validate") {
		t.Fatalf("expected the existing command listing to still be present, got:\n%s", stdout)
	}
}

func TestCLI_Version_ShowsDetailedBlock(t *testing.T) {
	bin := buildBinary(t)
	stdout, _, code := runCLI(t, bin, t.TempDir(), "--version")
	if code != 0 {
		t.Fatalf("expected exit 0 for --version, got %d", code)
	}
	for _, want := range []string{"rest-o-matic v", "commit:", "commit time:", "dirty:", "go version:"} {
		if !contains(stdout, want) {
			t.Errorf("expected --version output to contain %q, got:\n%s", want, stdout)
		}
	}
}

func TestCLI_VersionShorthand_MatchesLongFlag(t *testing.T) {
	bin := buildBinary(t)
	longOut, _, longCode := runCLI(t, bin, t.TempDir(), "--version")
	shortOut, _, shortCode := runCLI(t, bin, t.TempDir(), "-v")
	if longCode != 0 || shortCode != 0 {
		t.Fatalf("expected both to exit 0, got --version=%d -v=%d", longCode, shortCode)
	}
	if longOut != shortOut {
		t.Fatalf("expected -v and --version to produce identical output.\n--version: %q\n-v: %q", longOut, shortOut)
	}
}
