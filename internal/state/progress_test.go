package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProgress_SaveLoadRemove(t *testing.T) {
	store, dir := newTestStore(t)
	if p := store.LoadProgress("docs"); p != nil {
		t.Fatalf("expected no progress before any was saved, got %+v", p)
	}
	if _, err := os.Stat(filepath.Join(dir, "progress")); !os.IsNotExist(err) {
		t.Fatalf("LoadProgress created the progress directory: %v", err)
	}

	moved := time.Date(2026, 10, 21, 9, 10, 0, 0, time.UTC)
	left := int64(90)
	want := Progress{Phase: "backup", Repository: "s3", RepositoryIndex: 1, Repositories: 2,
		PercentDone: 0.43, BytesDone: 43, TotalBytes: 100, FilesDone: 4, TotalFiles: 10,
		SecondsRemaining: &left, CurrentFiles: []string{"/a"},
		Updated: moved.Add(time.Minute), LastReport: &moved, LastMoved: &moved}
	if err := store.SaveProgress("docs", want); err != nil {
		t.Fatalf("SaveProgress: %v", err)
	}
	got := store.LoadProgress("docs")
	if got == nil || got.Repository != "s3" || got.BytesDone != 43 || *got.SecondsRemaining != 90 || !got.LastMoved.Equal(moved) {
		t.Fatalf("got %+v", got)
	}
	// No temp files are left beside it.
	entries, _ := os.ReadDir(filepath.Join(dir, "progress"))
	if len(entries) != 1 || entries[0].Name() != "docs.json" {
		t.Fatalf("progress dir holds %v", entries)
	}

	if err := store.RemoveProgress("docs"); err != nil {
		t.Fatalf("RemoveProgress: %v", err)
	}
	if p := store.LoadProgress("docs"); p != nil {
		t.Fatalf("expected no progress after removing it, got %+v", p)
	}
	if err := store.RemoveProgress("docs"); err != nil {
		t.Fatalf("removing a missing record: %v", err)
	}
}

func TestProgress_DamagedRecordReadsAsNone(t *testing.T) {
	store, dir := newTestStore(t)
	if err := os.MkdirAll(filepath.Join(dir, "progress"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "progress", "docs.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := store.LoadProgress("docs"); p != nil {
		t.Fatalf("got %+v", p)
	}
}
