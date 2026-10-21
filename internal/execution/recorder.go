package execution

import (
	"sync"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// progressWriteInterval is the least time between two writes of restic's
// figures to a job's progress record. It bounds how stale `status` can
// be; phase changes are written at once.
const progressWriteInterval = 5 * time.Second

// ProgressSaver is where a Recorder writes; *state.Store is one.
type ProgressSaver interface {
	SaveProgress(jobName string, p state.Progress) error
}

// Recorder keeps one running job's progress record up to date for
// `status` to read. It is a Progress for that job alone.
type Recorder struct {
	job   string
	saver ProgressSaver
	now   func() time.Time
	// warn is told of the first write that fails; later failures are
	// dropped, since progress is only ever shown.
	warn func(error)

	mu        sync.Mutex
	cur       state.Progress
	lastWrite time.Time
	warned    bool
}

// NewRecorder returns a Recorder for job. now is the clock (time.Now when
// nil), and warn, when set, is told once if writing fails.
func NewRecorder(job string, saver ProgressSaver, now func() time.Time, warn func(error)) *Recorder {
	if now == nil {
		now = time.Now
	}
	return &Recorder{job: job, saver: saver, now: now, warn: warn}
}

// Phase records a change of phase and writes it at once. The backup
// figures start again from nothing with each new phase.
func (r *Recorder) Phase(job string, p Phase) {
	if job != r.job {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cur = PhaseProgress(p)
	r.write()
}

// PhaseProgress is the progress at the start of phase p: where the job
// is, with no figures yet.
func PhaseProgress(p Phase) state.Progress {
	return state.Progress{Phase: p.Name, Repository: p.Repository, RepositoryIndex: p.Index, Repositories: p.Count}
}

// ApplyStatus updates cur with one of restic's reports, received at now:
// the figures are replaced, LastReport is now, and LastMoved is now when
// the bytes or files done went up (or on the first report).
func ApplyStatus(cur *state.Progress, s BackupStatus, now time.Time) {
	moved := cur.LastMoved == nil || s.BytesDone > cur.BytesDone || s.FilesDone > cur.FilesDone
	cur.PercentDone = s.PercentDone
	cur.BytesDone, cur.TotalBytes = s.BytesDone, s.TotalBytes
	cur.FilesDone, cur.TotalFiles = s.FilesDone, s.TotalFiles
	cur.SecondsRemaining = s.SecondsRemaining
	if len(s.CurrentFiles) > 0 {
		cur.CurrentFiles = s.CurrentFiles
	}
	cur.LastReport = &now
	if moved {
		cur.LastMoved = &now
	}
}

// BackupStatus records one of restic's reports. The first of a phase is
// written at once, so that status has figures to show as soon as restic
// has any; after that, when progressWriteInterval has passed since the
// last write.
func (r *Recorder) BackupStatus(job string, s BackupStatus) {
	if job != r.job {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	first := r.cur.LastReport == nil
	ApplyStatus(&r.cur, s, now)
	if first || now.Sub(r.lastWrite) >= progressWriteInterval {
		r.write()
	}
}

// write saves the current record. The caller holds r.mu.
func (r *Recorder) write() {
	now := r.now()
	r.cur.Updated = now
	r.lastWrite = now
	if err := r.saver.SaveProgress(r.job, r.cur); err != nil && !r.warned {
		r.warned = true
		if r.warn != nil {
			r.warn(err)
		}
	}
}
