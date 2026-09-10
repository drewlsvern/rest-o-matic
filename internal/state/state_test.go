package state

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLoad_MissingFileReturnsEmptyState(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "state.json"))

	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(st.Jobs) != 0 {
		t.Fatalf("expected empty jobs map, got %v", st.Jobs)
	}
}

func TestUpdate_PersistsAcrossLoads(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "state.json"))

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.Update("postgres", JobState{LastRun: now, LastOutcome: "success"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := st.Jobs["postgres"]
	if !ok {
		t.Fatal("expected postgres job state to be present")
	}
	if !got.LastRun.Equal(now) || got.LastOutcome != "success" {
		t.Fatalf("got %+v, want LastRun=%v LastOutcome=success", got, now)
	}
}

func TestUpdate_ConcurrentWritesForDifferentJobsAllPersist(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "state.json"))

	var wg sync.WaitGroup
	jobs := []string{"documents", "postgres", "mysql", "logs"}
	for _, j := range jobs {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_ = s.Update(name, JobState{LastRun: time.Now(), LastOutcome: "success"})
		}(j)
	}
	wg.Wait()

	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, j := range jobs {
		if _, ok := st.Jobs[j]; !ok {
			t.Fatalf("expected job %q to be recorded after concurrent updates, got %v", j, st.Jobs)
		}
	}
}
