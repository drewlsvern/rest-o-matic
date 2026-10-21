package execution

import (
	"bytes"
	"encoding/json"
	"sync"
)

// The phases a job goes through, in order. A job with several
// repositories goes through backup and forget once for each.
const (
	PhaseBeforeHooks = "before_hooks"
	PhaseBackup      = "backup"
	PhaseForget      = "forget"
	PhaseAfterHooks  = "after_hooks"
)

// Phase is where a running job is. Repository, Index (1-based) and Count
// are set for the backup and forget phases.
type Phase struct {
	Name       string
	Repository string
	Index      int
	Count      int
}

// BackupStatus is one of restic's progress reports during a backup, as
// restic gives it. SecondsRemaining is nil until restic can estimate it.
type BackupStatus struct {
	PercentDone      float64  `json:"percent_done"`
	TotalFiles       int64    `json:"total_files"`
	FilesDone        int64    `json:"files_done"`
	TotalBytes       int64    `json:"total_bytes"`
	BytesDone        int64    `json:"bytes_done"`
	SecondsElapsed   int64    `json:"seconds_elapsed"`
	SecondsRemaining *int64   `json:"seconds_remaining"`
	CurrentFiles     []string `json:"current_files"`
}

// Progress receives a running job's progress. Its methods may be called
// from more than one goroutine, and must not block for long: they are
// called while restic's output is being read.
type Progress interface {
	Phase(job string, p Phase)
	BackupStatus(job string, s BackupStatus)
}

// Progresses passes everything it receives to each of ps; nil entries
// are skipped.
type Progresses []Progress

func (ps Progresses) Phase(job string, p Phase) {
	for _, x := range ps {
		if x != nil {
			x.Phase(job, p)
		}
	}
}

func (ps Progresses) BackupStatus(job string, s BackupStatus) {
	for _, x := range ps {
		if x != nil {
			x.BackupStatus(job, s)
		}
	}
}

// backupOutput reads restic backup's --json standard output as it is
// written, one line at a time: status lines go to onStatus, and the
// summary line's snapshot id is kept. Nothing else is held on to, so a
// day-long backup's thousands of status lines don't pile up in memory.
type backupOutput struct {
	mu         sync.Mutex
	partial    []byte
	snapshotID string
	onStatus   func(BackupStatus)
}

func (o *backupOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.partial = append(o.partial, p...)
	for {
		i := bytes.IndexByte(o.partial, '\n')
		if i < 0 {
			break
		}
		o.line(o.partial[:i])
		o.partial = o.partial[i+1:]
	}
	// Keep the buffer from growing without bound on output with no
	// newlines at all; restic's lines are far shorter than this.
	if len(o.partial) > 16<<20 {
		o.partial = nil
	}
	return len(p), nil
}

// close handles a last line that had no newline.
func (o *backupOutput) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.line(o.partial)
	o.partial = nil
}

func (o *backupOutput) line(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var msg struct {
		MessageType string `json:"message_type"`
		SnapshotID  string `json:"snapshot_id"`
	}
	if json.Unmarshal(line, &msg) != nil {
		return
	}
	switch msg.MessageType {
	case "summary":
		o.snapshotID = msg.SnapshotID
	case "status":
		if o.onStatus == nil {
			return
		}
		var s BackupStatus
		if json.Unmarshal(line, &s) == nil {
			o.onStatus(s)
		}
	}
}
