package statedir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	me    = 1000
	other = 0
)

// fakeOwners makes Check see the running user as `me` and every path as
// owned by `me`, except the base names listed in owners. It restores the
// real hooks when the test ends.
func fakeOwners(t *testing.T, owners map[string]int) {
	t.Helper()
	origUID, origOwner := currentUID, ownerOf
	t.Cleanup(func() { currentUID, ownerOf = origUID, origOwner })
	currentUID = func() (int, bool) { return me, true }
	ownerOf = func(info os.FileInfo) int {
		if uid, ok := owners[info.Name()]; ok {
			return uid
		}
		return me
	}
}

// newStateDir creates a state directory holding a lock file and a state
// file, laid out like the real one.
func newStateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(filepath.Join(dir, LocksDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(LocksDir, "repo-nas.lock"), StateFile, "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, p), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func ownerError(t *testing.T, err error) *OwnerError {
	t.Helper()
	var oe *OwnerError
	if !errors.As(err, &oe) {
		t.Fatalf("expected an *OwnerError, got %v", err)
	}
	return oe
}

func TestCheck_MissingDirectoryPasses(t *testing.T) {
	fakeOwners(t, map[string]int{"state": other})
	if err := Check(filepath.Join(t.TempDir(), "state")); err != nil {
		t.Fatalf("expected a missing state directory to pass, got %v", err)
	}
}

func TestCheck_AllOwnedByRunningUserPasses(t *testing.T) {
	fakeOwners(t, nil)
	if err := Check(newStateDir(t)); err != nil {
		t.Fatalf("expected a matching state directory to pass, got %v", err)
	}
}

func TestCheck_DirectoryOwnedByAnotherUserFails(t *testing.T) {
	dir := newStateDir(t)
	fakeOwners(t, map[string]int{"state": other, LocksDir: other, "repo-nas.lock": other, StateFile: other})
	oe := ownerError(t, Check(dir))
	if oe.DirOwned {
		t.Error("expected DirOwned to be false when the directory itself has another owner")
	}
	if len(oe.Mismatches) != 4 || oe.Mismatches[0].Path != dir {
		t.Errorf("expected the directory first and all four paths listed, got %+v", oe.Mismatches)
	}
	if msg := oe.Error(); !strings.Contains(msg, "--state-dir") || !strings.Contains(msg, "run rest-o-matic as") {
		t.Errorf("expected the message to suggest running as the owner or --state-dir, got:\n%s", msg)
	}
}

func TestCheck_LeftoverLockFileFails(t *testing.T) {
	dir := newStateDir(t)
	fakeOwners(t, map[string]int{"repo-nas.lock": other})
	oe := ownerError(t, Check(dir))
	want := filepath.Join(dir, LocksDir, "repo-nas.lock")
	if len(oe.Mismatches) != 1 || oe.Mismatches[0].Path != want {
		t.Fatalf("expected only %s listed, got %+v", want, oe.Mismatches)
	}
	if !oe.DirOwned {
		t.Error("expected DirOwned to be true when only a file inside has another owner")
	}
	if msg := oe.Error(); !strings.Contains(msg, "remove them, or change their owner") || strings.Contains(msg, "--state-dir") {
		t.Errorf("expected the remove-or-chown fix, got:\n%s", msg)
	}
}

func TestCheck_LeftoverStateFileFails(t *testing.T) {
	dir := newStateDir(t)
	fakeOwners(t, map[string]int{StateFile: other})
	oe := ownerError(t, Check(dir))
	if len(oe.Mismatches) != 1 || oe.Mismatches[0].Path != filepath.Join(dir, StateFile) {
		t.Fatalf("expected only the state file listed, got %+v", oe.Mismatches)
	}
}

func TestCheck_UnrelatedFilesIgnored(t *testing.T) {
	dir := newStateDir(t)
	fakeOwners(t, map[string]int{"notes.txt": other})
	if err := Check(dir); err != nil {
		t.Fatalf("expected files outside the checked layout to be ignored, got %v", err)
	}
}

func TestCheck_SkippedWithoutUnixOwnership(t *testing.T) {
	dir := newStateDir(t)
	fakeOwners(t, map[string]int{"state": other})
	currentUID = func() (int, bool) { return 0, false }
	if err := Check(dir); err != nil {
		t.Fatalf("expected no check where ownership isn't supported, got %v", err)
	}
}
