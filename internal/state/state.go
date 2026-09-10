// Package state persists per-job last-run information in a local JSON file,
// so that "tick" invocations - each a separate, one-shot process - can
// determine due-ness without any process staying resident between them.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JobState is what's recorded about a single job's most recent execution.
type JobState struct {
	LastRun     time.Time `json:"last_run"`
	LastOutcome string    `json:"last_outcome"`
}

// State is the full contents of the state file.
type State struct {
	Jobs map[string]JobState `json:"jobs"`
}

// Store reads and writes the state file, guarding writes with an in-process
// mutex so concurrent goroutines (e.g. jobs dispatched by one tick) don't
// race on the read-modify-write cycle.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a Store backed by the JSON file at path. The file need
// not exist yet; Load returns an empty State in that case.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads the current state. A missing file is not an error - it means
// no job has ever run.
func (s *Store) Load() (*State, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Jobs: map[string]JobState{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state %s: %w", s.path, err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parsing state %s: %w", s.path, err)
	}
	if st.Jobs == nil {
		st.Jobs = map[string]JobState{}
	}
	return &st, nil
}

// Update sets a single job's state and writes the file back atomically.
func (s *Store) Update(jobName string, js JobState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.Load()
	if err != nil {
		return err
	}
	st.Jobs[jobName] = js
	return s.writeAtomic(st)
}

// writeAtomic writes state to a temp file in the same directory and renames
// it over the target path, so a crash mid-write never leaves a truncated or
// corrupt state file behind. Callers must hold s.mu.
func (s *Store) writeAtomic(st *State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating state dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp state file into place: %w", err)
	}
	return nil
}
