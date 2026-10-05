package central

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEnrolment_SaveLoadRemove(t *testing.T) {
	path := EnrolmentPath(filepath.Join(t.TempDir(), "keys", "host.key"))
	if filepath.Base(path) != "enrolment.json" {
		t.Fatalf("path %s", path)
	}
	if _, err := LoadEnrolment(path); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("missing file: %v", err)
	}
	e := &Enrolment{URL: "https://x", HostID: "h", HostName: "n", Credential: "c", ConfigPath: "/c.yaml", EnrolledAt: time.Now().UTC().Truncate(time.Second)}
	if err := SaveEnrolment(path, e); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, want 0600", info.Mode().Perm())
		}
	}
	got, err := LoadEnrolment(path)
	if err != nil || *got != *e {
		t.Fatalf("got %+v, %v; want %+v", got, err, e)
	}
	if err := RemoveEnrolment(path); err != nil {
		t.Fatal(err)
	}
	if err := RemoveEnrolment(path); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestEnrolment_RefusedWhenOthersCanRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no permission bits on Windows")
	}
	path := filepath.Join(t.TempDir(), "enrolment.json")
	if err := SaveEnrolment(path, &Enrolment{URL: "https://x", HostID: "h", Credential: "c", ConfigPath: "/c"}); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0o644)
	if _, err := LoadEnrolment(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("got %v, want it refused", err)
	}
}
