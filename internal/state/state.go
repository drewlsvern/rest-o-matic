// Package state persists per-job run information in a local JSON file, so
// that "tick" invocations - each a separate, one-shot process - can
// determine due-ness without any process staying resident between them, and
// so that "status" can report what happened without consulting any log.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/lock"
)

// Values recorded in the state file.
const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"

	TriggerTick = "tick"
	TriggerRun  = "run"

	ResultOK           = "ok"
	ResultBackupFailed = "backup_failed"
	ResultForgetFailed = "forget_failed"
)

// MaxRuns is how many of a job's most recent runs are kept.
const MaxRuns = 20

// RepoResult is what one run did against one repository.
type RepoResult struct {
	Name   string `json:"name"`
	Result string `json:"result"`
	// Error is a one-line description of the failure, empty when Result is
	// ResultOK.
	Error string `json:"error,omitempty"`
	// SnapshotID is the snapshot the backup created, if it got that far.
	SnapshotID string `json:"snapshot_id,omitempty"`
}

// RunRecord is one finished execution of a job.
type RunRecord struct {
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Outcome  string    `json:"outcome"`
	Trigger  string    `json:"trigger"`
	// Error is a one-line description of the run's first failure, empty on
	// success.
	Error string `json:"error,omitempty"`
	// Repositories holds a result for each repository the run attempted;
	// empty when it never got as far as a backup.
	Repositories []RepoResult `json:"repositories,omitempty"`
}

// Running marks a job whose execution has begun. It is only to be believed
// while the job's lock is held (see lock.JobHeld): a killed process leaves
// it behind.
type Running struct {
	Started time.Time `json:"started"`
	Trigger string    `json:"trigger"`
}

// JobState is what's recorded about a single job.
type JobState struct {
	// LastRun (when the most recent run finished) and LastOutcome are what
	// due-ness is computed from. They are the whole of what earlier
	// versions wrote, and are kept as they were so those files stay valid.
	LastRun     time.Time `json:"last_run,omitzero"`
	LastOutcome string    `json:"last_outcome,omitempty"`

	Running *Running `json:"running,omitempty"`
	// FailingSince is when the first run of the current run of failures
	// started, and FailureNotified when the failure notification was last
	// sent for it. Both are cleared when the job next succeeds.
	FailingSince    *time.Time `json:"failing_since,omitempty"`
	FailureNotified *time.Time `json:"failure_notified,omitempty"`
	// Runs are the most recent runs, newest first, at most MaxRuns.
	Runs []RunRecord `json:"runs,omitempty"`
}

// State is the full contents of the state file.
type State struct {
	// LastTick is when `tick` last got as far as evaluating schedules.
	LastTick *time.Time          `json:"last_tick,omitempty"`
	Jobs     map[string]JobState `json:"jobs"`
	// Checkin is what is known about check-ins with the central app; nil
	// before the first.
	Checkin *CheckinState `json:"checkin,omitempty"`
	// Restic caches restic's version, which every check-in reports.
	Restic *ResticVersion `json:"restic,omitempty"`
}

// CheckinState is the record of check-ins for one enrolment.
type CheckinState struct {
	// HostID is the enrolment this record belongs to. A record for another
	// host ID is from an earlier enrolment and counts for nothing.
	HostID      string     `json:"host_id"`
	LastAttempt *time.Time `json:"last_attempt,omitempty"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
	// LastError is why the most recent attempt failed; empty when it
	// succeeded.
	LastError string `json:"last_error,omitempty"`
	// Acknowledged is the fingerprint the central app last received of
	// each part, by part name.
	Acknowledged map[string]string `json:"acknowledged,omitempty"`
	// Resend lists parts the central app asked to receive in full again.
	Resend []string `json:"resend,omitempty"`
}

// CheckinFor returns the check-in record for the enrolment with hostID,
// or an empty one if there is none yet.
func (st *State) CheckinFor(hostID string) CheckinState {
	if st.Checkin == nil || st.Checkin.HostID != hostID {
		return CheckinState{HostID: hostID}
	}
	return *st.Checkin
}

// ResticVersion is restic's version, together with what identifies the
// binary it was read from, so it is read again only when restic changes.
type ResticVersion struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Version string    `json:"version"`
}

// Store reads and writes the state file. Every write is a read-modify-write
// guarded twice: by an in-process mutex, and by a file lock so that
// separate processes (an overlapping tick, a manual run) can't lose each
// other's updates.
type Store struct {
	path    string
	lockDir string
	mu      sync.Mutex
}

// NewStore returns a Store backed by the JSON file at path, using lockDir
// for the lock that guards writes. Neither need exist yet; Load returns an
// empty State when the file is missing.
func NewStore(path, lockDir string) *Store {
	return &Store{path: path, lockDir: lockDir}
}

// Load reads the current state. A missing file is not an error - it means
// no job has ever run. Load never creates or locks anything.
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

// Failing is a job's failing state as of a run that failed: since when it
// has been failing, and when its failure notification was last sent (nil
// if it hasn't been).
type Failing struct {
	Since    *time.Time
	Notified *time.Time
}

// RecordRun adds a finished run to the front of the job's history, dropping
// the oldest beyond MaxRuns, and clears its running marker. failing is
// recorded with a failed run; a successful run clears it whatever is given.
func (s *Store) RecordRun(jobName string, run RunRecord, failing Failing) error {
	return s.update(func(st *State) bool {
		js := st.Jobs[jobName]
		js.LastRun = run.Finished
		js.LastOutcome = run.Outcome
		js.Running = nil
		js.FailingSince, js.FailureNotified = failing.Since, failing.Notified
		if run.Outcome == OutcomeSuccess {
			js.FailingSince, js.FailureNotified = nil, nil
		}
		js.Runs = append([]RunRecord{run}, js.Runs...)
		if len(js.Runs) > MaxRuns {
			js.Runs = js.Runs[:MaxRuns]
		}
		st.Jobs[jobName] = js
		return true
	})
}

// MarkRunning records that an execution of the job has begun.
func (s *Store) MarkRunning(jobName string, started time.Time, trigger string) error {
	return s.update(func(st *State) bool {
		js := st.Jobs[jobName]
		js.Running = &Running{Started: started, Trigger: trigger}
		st.Jobs[jobName] = js
		return true
	})
}

// ClearRunning removes a running marker left behind by an execution that
// was killed. The caller must hold the job's lock, which is what proves no
// such execution is still alive. Nothing is written if there is no marker.
func (s *Store) ClearRunning(jobName string) error {
	return s.update(func(st *State) bool {
		js, ok := st.Jobs[jobName]
		if !ok || js.Running == nil {
			return false
		}
		js.Running = nil
		st.Jobs[jobName] = js
		return true
	})
}

// RecordTick records when a tick evaluated job schedules.
func (s *Store) RecordTick(t time.Time) error {
	return s.update(func(st *State) bool {
		st.LastTick = &t
		return true
	})
}

// RecordCheckin records a check-in attempt at at for the enrolment with
// hostID. On success (err nil) every part in sent is recorded as
// acknowledged with its fingerprint, and resend replaces the parts the
// central app wants again; on failure only the error is recorded, so the
// same parts are sent next time. It reports whether the attempt before
// this one had failed.
func (s *Store) RecordCheckin(hostID string, at time.Time, sent map[string]string, resend []string, err error) (wasFailing bool, _ error) {
	updateErr := s.update(func(st *State) bool {
		c := st.CheckinFor(hostID)
		wasFailing = c.LastError != ""
		c.LastAttempt = &at
		if err != nil {
			c.LastError = err.Error()
		} else {
			c.LastSuccess, c.LastError = &at, ""
			acked := map[string]string{}
			for part, fp := range c.Acknowledged {
				acked[part] = fp
			}
			for part, fp := range sent {
				acked[part] = fp
			}
			c.Acknowledged, c.Resend = acked, resend
		}
		st.Checkin = &c
		return true
	})
	return wasFailing, updateErr
}

// RecordResticVersion caches restic's version.
func (s *Store) RecordResticVersion(v ResticVersion) error {
	return s.update(func(st *State) bool {
		st.Restic = &v
		return true
	})
}

// update is the one way the state file is written: it takes the
// cross-process state lock, loads the file, applies change, and writes the
// result back atomically if change reports that it altered anything.
func (s *Store) update(change func(*State) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok, err := lock.AcquireState(s.lockDir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("state %s is locked by another process", s.path)
	}
	defer l.Unlock()

	st, err := s.Load()
	if err != nil {
		return err
	}
	if !change(st) {
		return nil
	}
	return s.writeAtomic(st)
}

// writeAtomic writes state to a temp file in the same directory and renames
// it over the target path, so a crash mid-write never leaves a truncated or
// corrupt state file behind. Callers must hold s.mu and the state lock.
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
