package execution

import (
	"errors"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// memSaver keeps every record written, and can be made to fail.
type memSaver struct {
	saved []state.Progress
	err   error
}

func (m *memSaver) SaveProgress(_ string, p state.Progress) error {
	if m.err != nil {
		return m.err
	}
	m.saved = append(m.saved, p)
	return nil
}

// fakeClock is a clock tests move by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestRecorder_PhasesAtOnceFiguresThrottled(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	saver := &memSaver{}
	r := NewRecorder("docs", saver, clock.now, nil)

	r.Phase("docs", Phase{Name: PhaseBackup, Repository: "s3", Index: 1, Count: 2})
	if len(saver.saved) != 1 || saver.saved[0].Repository != "s3" || saver.saved[0].RepositoryIndex != 1 {
		t.Fatalf("phase change not written at once: %+v", saver.saved)
	}

	// The first report is written at once...
	r.BackupStatus("docs", BackupStatus{BytesDone: 5, TotalBytes: 100})
	if len(saver.saved) != 2 || saver.saved[1].BytesDone != 5 {
		t.Fatalf("first report not written at once: %+v", saver.saved)
	}
	saver.saved = saver.saved[:1]
	// ...reports within 5s of the last write are held back...
	for i := 1; i <= 4; i++ {
		clock.advance(time.Second)
		r.BackupStatus("docs", BackupStatus{BytesDone: int64(i * 10), TotalBytes: 100})
	}
	if len(saver.saved) != 1 {
		t.Fatalf("figures written before 5s had passed: %d writes", len(saver.saved))
	}
	// ...and the next one after 5s is written, with the latest figures.
	clock.advance(time.Second)
	r.BackupStatus("docs", BackupStatus{BytesDone: 50, TotalBytes: 100})
	if len(saver.saved) != 2 || saver.saved[1].BytesDone != 50 || saver.saved[1].Phase != PhaseBackup {
		t.Fatalf("got %+v", saver.saved)
	}

	// Another job's progress is not this recorder's.
	r.Phase("other", Phase{Name: PhaseAfterHooks})
	if len(saver.saved) != 2 {
		t.Fatalf("recorded another job's phase: %+v", saver.saved)
	}

	// A new phase starts the figures from nothing.
	r.Phase("docs", Phase{Name: PhaseForget, Repository: "s3", Index: 1, Count: 2})
	if last := saver.saved[len(saver.saved)-1]; last.Phase != PhaseForget || last.BytesDone != 0 || last.LastReport != nil {
		t.Fatalf("got %+v", last)
	}
}

func TestRecorder_LastMovedHoldsWhileFiguresStandStill(t *testing.T) {
	start := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: start}
	saver := &memSaver{}
	r := NewRecorder("docs", saver, clock.now, nil)
	r.Phase("docs", Phase{Name: PhaseBackup, Repository: "s3", Index: 1, Count: 1})
	r.BackupStatus("docs", BackupStatus{BytesDone: 100, FilesDone: 3})
	for i := 0; i < 120; i++ { // ten minutes of reports with nothing moving
		clock.advance(5 * time.Second)
		r.BackupStatus("docs", BackupStatus{BytesDone: 100, FilesDone: 3})
	}
	last := saver.saved[len(saver.saved)-1]
	if !last.LastMoved.Equal(start) || !last.LastReport.Equal(start.Add(10*time.Minute)) {
		t.Fatalf("last moved %v, last report %v", last.LastMoved, last.LastReport)
	}
	// Files moving counts as moving, even with the bytes still.
	clock.advance(5 * time.Second)
	r.BackupStatus("docs", BackupStatus{BytesDone: 100, FilesDone: 4})
	if last := saver.saved[len(saver.saved)-1]; !last.LastMoved.Equal(clock.t) {
		t.Fatalf("last moved %v, want %v", last.LastMoved, clock.t)
	}
}

func TestRecorder_WriteFailureWarnedOnce(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	saver := &memSaver{err: errors.New("disk full")}
	var warnings []error
	r := NewRecorder("docs", saver, clock.now, func(err error) { warnings = append(warnings, err) })
	r.Phase("docs", Phase{Name: PhaseBeforeHooks})
	r.Phase("docs", Phase{Name: PhaseBackup, Repository: "s3", Index: 1, Count: 1})
	clock.advance(time.Minute)
	r.BackupStatus("docs", BackupStatus{BytesDone: 1})
	if len(warnings) != 1 || warnings[0].Error() != "disk full" {
		t.Fatalf("got warnings %v", warnings)
	}
}

func TestRecorder_RetryWrittenAtOnce(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	saver := &memSaver{}
	r := NewRecorder("docs", saver, clock.now, nil)
	r.Phase("docs", Phase{Name: PhaseCheck, Repository: "s3", Index: 1, Count: 2})
	clock.advance(time.Second)
	r.Retry("docs", "dial tcp 192.0.2.1:443: i/o timeout")
	if len(saver.saved) != 2 {
		t.Fatalf("retry not written at once: %+v", saver.saved)
	}
	last := saver.saved[1]
	if last.Phase != PhaseCheck || last.LastRetry != "dial tcp 192.0.2.1:443: i/o timeout" || !last.LastRetryAt.Equal(clock.t) || !last.PhaseStarted.Equal(clock.t.Add(-time.Second)) {
		t.Fatalf("got %+v", last)
	}
	// A new phase starts without the old retry.
	r.Phase("docs", Phase{Name: PhaseBackup, Repository: "s3", Index: 1, Count: 2})
	if last := saver.saved[len(saver.saved)-1]; last.LastRetry != "" || last.LastRetryAt != nil {
		t.Fatalf("retry carried into the next phase: %+v", last)
	}
}
