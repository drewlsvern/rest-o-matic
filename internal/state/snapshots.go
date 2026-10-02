package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// snapshotsDirName is the directory, beside the state file, holding one
// snapshot list file per job.
const snapshotsDirName = "snapshots"

// Snapshot is one restic snapshot as recorded in a job's snapshot list.
type Snapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Paths    []string  `json:"paths"`
	Tags     []string  `json:"tags"`
	// Size is the bytes the backup processed and Files the files it
	// processed. Both are nil when restic doesn't report them (it doesn't
	// before 0.17).
	Size  *int64 `json:"size,omitempty"`
	Files *int64 `json:"files,omitempty"`
	// What this snapshot changed compared with the one before it: files
	// that were new, files that had changed, and the bytes it added to the
	// repository before (DataAdded) and after (DataAddedPacked)
	// compression. All nil when restic doesn't report them.
	FilesNew        *int64 `json:"files_new,omitempty"`
	FilesChanged    *int64 `json:"files_changed,omitempty"`
	DataAdded       *int64 `json:"data_added,omitempty"`
	DataAddedPacked *int64 `json:"data_added_packed,omitempty"`
}

// SnapshotList is the snapshots one job has in one repository, newest
// first, as of ListedAt.
type SnapshotList struct {
	ListedAt  time.Time  `json:"listed_at"`
	Snapshots []Snapshot `json:"snapshots"`
}

// jobSnapshots is the contents of one job's snapshot list file.
type jobSnapshots struct {
	Repositories map[string]SnapshotList `json:"repositories"`
}

func (s *Store) snapshotsPath(jobName string) string {
	return filepath.Join(filepath.Dir(s.path), snapshotsDirName, jobName+".json")
}

// LoadSnapshots returns the snapshot lists recorded for a job, keyed by
// repository. A job with none recorded yet gets an empty map. It never
// creates anything.
func (s *Store) LoadSnapshots(jobName string) (map[string]SnapshotList, error) {
	path := s.snapshotsPath(jobName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]SnapshotList{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading snapshot lists %s: %w", path, err)
	}
	var js jobSnapshots
	if err := json.Unmarshal(data, &js); err != nil {
		return nil, fmt.Errorf("parsing snapshot lists %s: %w", path, err)
	}
	if js.Repositories == nil {
		js.Repositories = map[string]SnapshotList{}
	}
	return js.Repositories, nil
}

// SaveSnapshots records the list for one of a job's repositories, leaving
// the job's other repositories as they were.
//
// The caller must hold the job's lock. That is what makes this
// read-modify-write safe without a lock of its own: the lists live in a
// file per job, outside the state file, so that the state file (rewritten
// on every tick) stays small.
func (s *Store) SaveSnapshots(jobName, repoName string, list SnapshotList) error {
	lists, err := s.LoadSnapshots(jobName)
	if err != nil {
		// A file that can't be read is replaced rather than left to block
		// every later save; its lists refresh as the job's repositories run.
		lists = map[string]SnapshotList{}
	}
	if list.Snapshots == nil {
		list.Snapshots = []Snapshot{}
	}
	lists[repoName] = list

	data, err := json.MarshalIndent(jobSnapshots{Repositories: lists}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding snapshot lists: %w", err)
	}
	path := s.snapshotsPath(jobName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating snapshot list dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+jobName+"-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp snapshot list file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp snapshot list file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp snapshot list file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp snapshot list file into place: %w", err)
	}
	return nil
}
