package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func snapshot(id string, at time.Time) Snapshot {
	size, files := int64(1024), int64(3)
	return Snapshot{ID: id + "0000", ShortID: id, Time: at, Hostname: "g5", Paths: []string{"/data"}, Tags: []string{"documents"}, Size: &size, Files: &files}
}

func TestSnapshots_MissingFileIsNoListsAndCreatesNothing(t *testing.T) {
	s, dir := newTestStore(t)

	lists, err := s.LoadSnapshots("documents")
	if err != nil || len(lists) != 0 {
		t.Fatalf("got %v, %v; want no lists and no error", lists, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "snapshots")); err == nil {
		t.Fatal("LoadSnapshots created the snapshots directory")
	}
}

func TestSnapshots_RoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	at := time.Date(2026, 10, 2, 2, 0, 5, 0, time.UTC)
	list := SnapshotList{ListedAt: at.Add(time.Minute), Snapshots: []Snapshot{snapshot("new", at), snapshot("old", at.Add(-24*time.Hour))}}

	if err := s.SaveSnapshots("documents", "nas", list); err != nil {
		t.Fatalf("SaveSnapshots: %v", err)
	}
	lists, err := s.LoadSnapshots("documents")
	if err != nil {
		t.Fatalf("LoadSnapshots: %v", err)
	}
	got := lists["nas"]
	if !got.ListedAt.Equal(list.ListedAt) || len(got.Snapshots) != 2 {
		t.Fatalf("got %+v, want the list as saved", got)
	}
	first := got.Snapshots[0]
	if first.ShortID != "new" || !first.Time.Equal(at) || first.Hostname != "g5" || first.Paths[0] != "/data" || first.Tags[0] != "documents" || *first.Size != 1024 || *first.Files != 3 {
		t.Fatalf("got first snapshot %+v, want every field back", first)
	}
}

func TestSnapshots_SavingOneRepositoryLeavesTheOthers(t *testing.T) {
	s, _ := newTestStore(t)
	t1 := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	if err := s.SaveSnapshots("documents", "nas", SnapshotList{ListedAt: t1, Snapshots: []Snapshot{snapshot("n1", t1)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshots("documents", "offsite", SnapshotList{ListedAt: t2, Snapshots: []Snapshot{snapshot("o1", t2)}}); err != nil {
		t.Fatal(err)
	}
	// A different job has its own file.
	if err := s.SaveSnapshots("postgres", "nas", SnapshotList{ListedAt: t2}); err != nil {
		t.Fatal(err)
	}

	lists, err := s.LoadSnapshots("documents")
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 || !lists["nas"].ListedAt.Equal(t1) || lists["nas"].Snapshots[0].ShortID != "n1" || lists["offsite"].Snapshots[0].ShortID != "o1" {
		t.Fatalf("got %+v, want nas unchanged beside the new offsite list", lists)
	}
}

// Sizes are unknown on older restic, and an empty list is a list.
func TestSnapshots_UnknownSizesAndEmptyLists(t *testing.T) {
	s, _ := newTestStore(t)
	at := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	if err := s.SaveSnapshots("documents", "nas", SnapshotList{ListedAt: at, Snapshots: []Snapshot{{ID: "abc", ShortID: "abc", Time: at}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshots("documents", "offsite", SnapshotList{ListedAt: at}); err != nil {
		t.Fatal(err)
	}

	lists, err := s.LoadSnapshots("documents")
	if err != nil {
		t.Fatal(err)
	}
	if got := lists["nas"].Snapshots[0]; got.Size != nil || got.Files != nil {
		t.Errorf("got size=%v files=%v, want both unknown", got.Size, got.Files)
	}
	if got, ok := lists["offsite"]; !ok || got.Snapshots == nil || len(got.Snapshots) != 0 {
		t.Errorf("got %+v (present=%v), want an empty list recorded", got, ok)
	}
}

func TestSnapshots_UnreadableFileIsReplacedOnSave(t *testing.T) {
	s, dir := newTestStore(t)
	path := filepath.Join(dir, "snapshots", "documents.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSnapshots("documents"); err == nil {
		t.Fatal("expected a damaged file to be reported when read")
	}

	at := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	if err := s.SaveSnapshots("documents", "nas", SnapshotList{ListedAt: at}); err != nil {
		t.Fatalf("SaveSnapshots over a damaged file: %v", err)
	}
	if lists, err := s.LoadSnapshots("documents"); err != nil || len(lists) != 1 {
		t.Fatalf("got %v, %v; want the damaged file replaced", lists, err)
	}
}
