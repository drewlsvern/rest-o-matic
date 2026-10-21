package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// progressDirName is the directory, beside the state file, holding one
// progress file for each job that is running.
const progressDirName = "progress"

// Progress is where a running job is, as last recorded. The backup
// figures are restic's own and are zero outside a backup.
type Progress struct {
	// Phase is before_hooks, backup, forget or after_hooks. Repository,
	// RepositoryIndex (1-based) and Repositories are set for backup and
	// forget.
	Phase           string `json:"phase"`
	Repository      string `json:"repository,omitempty"`
	RepositoryIndex int    `json:"repository_index,omitempty"`
	Repositories    int    `json:"repositories,omitempty"`

	PercentDone      float64  `json:"percent_done"`
	BytesDone        int64    `json:"bytes_done"`
	TotalBytes       int64    `json:"total_bytes"`
	FilesDone        int64    `json:"files_done"`
	TotalFiles       int64    `json:"total_files"`
	SecondsRemaining *int64   `json:"seconds_remaining,omitempty"`
	CurrentFiles     []string `json:"current_files,omitempty"`

	// Updated is when this record was written. LastReport is when restic
	// last sent a progress report, and LastMoved when its bytes or files
	// done last went up; both nil before the first report.
	Updated    time.Time  `json:"updated"`
	LastReport *time.Time `json:"last_report,omitempty"`
	LastMoved  *time.Time `json:"last_moved,omitempty"`
}

func (s *Store) progressPath(jobName string) string {
	return filepath.Join(filepath.Dir(s.path), progressDirName, jobName+".json")
}

// SaveProgress records a running job's progress, replacing what was
// recorded before.
//
// The caller must hold the job's lock: only one process then writes the
// file, so it needs no lock of its own, and in particular not the state
// lock, which would make every other job's state update compete with
// writes that come every few seconds. The file is replaced by a rename, so
// a reader sees the old record or the new one, never part of one.
func (s *Store) SaveProgress(jobName string, p Progress) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding progress: %w", err)
	}
	path := s.progressPath(jobName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating progress dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+jobName+"-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp progress file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp progress file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp progress file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp progress file into place: %w", err)
	}
	return nil
}

// LoadProgress returns a job's recorded progress, or nil when there is
// none or it can't be read: progress is only ever shown, so a missing or
// damaged record is the same as no record. It never creates anything.
//
// A record says nothing about whether the job is running; one left behind
// by a killed process is still here. Callers check the job's lock first.
func (s *Store) LoadProgress(jobName string) *Progress {
	data, err := os.ReadFile(s.progressPath(jobName))
	if err != nil {
		return nil
	}
	var p Progress
	if json.Unmarshal(data, &p) != nil {
		return nil
	}
	return &p
}

// RemoveProgress deletes a job's progress record, if it has one. The
// caller must hold the job's lock.
func (s *Store) RemoveProgress(jobName string) error {
	err := os.Remove(s.progressPath(jobName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing progress record: %w", err)
	}
	return nil
}
