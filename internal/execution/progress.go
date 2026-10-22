package execution

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sync"
)

// The phases a job goes through, in order. A job with several
// repositories goes through backup and forget once for each.
const (
	PhaseBeforeHooks = "before_hooks"
	PhaseCheck       = "check"
	PhaseBackup      = "backup"
	PhaseForget      = "forget"
	PhaseAfterHooks  = "after_hooks"
)

// Phase is where a running job is. Repository, Index (1-based) and Count
// are set for the check, backup and forget phases.
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
	// Retry is a message restic printed before retrying an operation,
	// while checking or backing up to a repository.
	Retry(job string, message string)
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

func (ps Progresses) Retry(job, message string) {
	for _, x := range ps {
		if x != nil {
			x.Retry(job, message)
		}
	}
}

// lineWriter is an io.Writer that hands each complete line written to it
// to onLine, as it arrives. restic's output is read this way so that its
// progress and its retries can be shown while it runs.
type lineWriter struct {
	mu      sync.Mutex
	partial []byte
	onLine  func(line []byte)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partial = append(w.partial, p...)
	for {
		i := bytes.IndexByte(w.partial, '\n')
		if i < 0 {
			break
		}
		w.onLine(w.partial[:i])
		w.partial = w.partial[i+1:]
	}
	// Keep the buffer from growing without bound on output with no
	// newlines at all; restic's lines are far shorter than this.
	if len(w.partial) > 16<<20 {
		w.partial = nil
	}
	return len(p), nil
}

// close handles a last line that had no newline.
func (w *lineWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.partial) > 0 {
		w.onLine(w.partial)
	}
	w.partial = nil
}

// backupOutput reads restic backup's --json standard output as it is
// written: status lines go to onStatus, and the summary line's snapshot id
// is kept. Nothing else is held on to, so a day-long backup's thousands of
// status lines don't pile up in memory.
type backupOutput struct {
	lineWriter
	snapshotID string
	onStatus   func(BackupStatus)
}

func newBackupOutput(onStatus func(BackupStatus)) *backupOutput {
	o := &backupOutput{onStatus: onStatus}
	o.onLine = o.line
	return o
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

// retryLine matches the line restic prints before retrying a failed
// backend operation, such as `Stat(<config/>) returned error, retrying
// after 1m13.7s: Stat: Get "…": dial tcp …: i/o timeout`; the group is the
// error, without restic's bookkeeping around it. restic 0.16 doesn't print
// these when it opens a repository.
var retryLine = regexp.MustCompile(`^.+? returned error, retrying after [0-9.]+[a-zµ]+(?:[0-9.]+[a-zµ]+)*: (.+)$`)

// stderrOutput collects restic's standard error whole, for reporting its
// errors once it exits, and passes each retry message to onRetry as it
// arrives.
type stderrOutput struct {
	lineWriter
	textMu  sync.Mutex
	text    bytes.Buffer
	onRetry func(message string)
	// lastRetry is the latest retry message, kept for when restic is
	// stopped before it reports why it couldn't go on.
	lastRetry string
}

func newStderrOutput(onRetry func(string)) *stderrOutput {
	o := &stderrOutput{onRetry: onRetry}
	o.onLine = o.line
	return o
}

func (o *stderrOutput) Write(p []byte) (int, error) {
	o.textMu.Lock()
	o.text.Write(p)
	o.textMu.Unlock()
	return o.lineWriter.Write(p)
}

func (o *stderrOutput) line(line []byte) {
	m := retryLine.FindSubmatch(bytes.TrimSpace(line))
	if m == nil {
		return
	}
	o.lastRetry = string(m[1])
	if o.onRetry != nil {
		o.onRetry(o.lastRetry)
	}
}

// String is everything restic wrote to standard error.
func (o *stderrOutput) String() string {
	o.textMu.Lock()
	defer o.textMu.Unlock()
	return o.text.String()
}
