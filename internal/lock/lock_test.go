package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestAcquireRepository_SecondAttemptDeferred(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireRepository(dir, "nas")
	if err != nil || !ok1 {
		t.Fatalf("first acquire: ok=%v err=%v", ok1, err)
	}
	defer l1.Unlock()

	_, ok2, err := AcquireRepository(dir, "nas")
	if err != nil {
		t.Fatalf("second acquire errored: %v", err)
	}
	if ok2 {
		t.Fatal("expected second acquire of the same repository to be deferred (ok=false)")
	}
}

func TestAcquireRepository_DifferentRepositoriesDoNotConflict(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireRepository(dir, "nas")
	if err != nil || !ok1 {
		t.Fatalf("nas acquire: ok=%v err=%v", ok1, err)
	}
	defer l1.Unlock()

	l2, ok2, err := AcquireRepository(dir, "offsite")
	if err != nil || !ok2 {
		t.Fatalf("offsite acquire: ok=%v err=%v", ok2, err)
	}
	defer l2.Unlock()
}

func TestAcquireRepository_ReleasedAfterUnlock(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireRepository(dir, "nas")
	if err != nil || !ok1 {
		t.Fatalf("first acquire: ok=%v err=%v", ok1, err)
	}
	if err := l1.Unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	l2, ok2, err := AcquireRepository(dir, "nas")
	if err != nil || !ok2 {
		t.Fatalf("re-acquire after unlock: ok=%v err=%v", ok2, err)
	}
	defer l2.Unlock()
}

func TestAcquireSlot_CapsConcurrentHolders(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireSlot(dir, 2)
	if err != nil || !ok1 {
		t.Fatalf("slot 1: ok=%v err=%v", ok1, err)
	}
	defer l1.Unlock()

	l2, ok2, err := AcquireSlot(dir, 2)
	if err != nil || !ok2 {
		t.Fatalf("slot 2: ok=%v err=%v", ok2, err)
	}
	defer l2.Unlock()

	_, ok3, err := AcquireSlot(dir, 2)
	if err != nil {
		t.Fatalf("slot 3 errored: %v", err)
	}
	if ok3 {
		t.Fatal("expected a third slot acquisition to fail when max_concurrent is 2")
	}
}

func TestAcquireSlot_FreesUpAfterUnlock(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireSlot(dir, 1)
	if err != nil || !ok1 {
		t.Fatalf("slot 1: ok=%v err=%v", ok1, err)
	}
	if err := l1.Unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	l2, ok2, err := AcquireSlot(dir, 1)
	if err != nil || !ok2 {
		t.Fatalf("re-acquire after unlock: ok=%v err=%v", ok2, err)
	}
	defer l2.Unlock()
}

func TestAcquireJob_SecondAttemptRefusedUntilUnlock(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireJob(dir, "gitea")
	if err != nil || !ok1 {
		t.Fatalf("first acquire: ok=%v err=%v", ok1, err)
	}

	if _, ok2, err := AcquireJob(dir, "gitea"); err != nil || ok2 {
		t.Fatalf("second acquire of a held job: ok=%v err=%v, want ok=false", ok2, err)
	}

	if err := l1.Unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	l3, ok3, err := AcquireJob(dir, "gitea")
	if err != nil || !ok3 {
		t.Fatalf("re-acquire after unlock: ok=%v err=%v", ok3, err)
	}
	defer l3.Unlock()
}

func TestAcquireJob_DifferentJobsDoNotConflict(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireJob(dir, "gitea")
	if err != nil || !ok1 {
		t.Fatalf("gitea acquire: ok=%v err=%v", ok1, err)
	}
	defer l1.Unlock()

	l2, ok2, err := AcquireJob(dir, "postgres")
	if err != nil || !ok2 {
		t.Fatalf("postgres acquire: ok=%v err=%v", ok2, err)
	}
	defer l2.Unlock()
}

// A job and a repository of the same name use different lock files.
func TestAcquireJob_DoesNotConflictWithRepositoryOfSameName(t *testing.T) {
	dir := t.TempDir()

	l1, ok1, err := AcquireJob(dir, "nas")
	if err != nil || !ok1 {
		t.Fatalf("job acquire: ok=%v err=%v", ok1, err)
	}
	defer l1.Unlock()

	l2, ok2, err := AcquireRepository(dir, "nas")
	if err != nil || !ok2 {
		t.Fatalf("repository acquire: ok=%v err=%v", ok2, err)
	}
	defer l2.Unlock()
}

func shortenWait(t *testing.T) {
	t.Helper()
	old := waitInterval
	waitInterval = 5 * time.Millisecond
	t.Cleanup(func() { waitInterval = old })
}

func TestWaitRepository_ReturnsOnceHolderUnlocks(t *testing.T) {
	shortenWait(t)
	dir := t.TempDir()

	held, ok, err := AcquireRepository(dir, "nas")
	if err != nil || !ok {
		t.Fatalf("setup acquire: ok=%v err=%v", ok, err)
	}

	waited := make(chan struct{})
	got := make(chan error, 1)
	go func() {
		l, err := WaitRepository(context.Background(), dir, "nas", func() { close(waited) })
		if err == nil {
			defer l.Unlock()
		}
		got <- err
	}()

	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("onWait was not called while the repository was held")
	}
	select {
	case err := <-got:
		t.Fatalf("WaitRepository returned while the repository was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := held.Unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("WaitRepository: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitRepository did not return after the holder unlocked")
	}
}

func TestWaitRepository_DoesNotCallOnWaitWhenFree(t *testing.T) {
	dir := t.TempDir()

	l, err := WaitRepository(context.Background(), dir, "nas", func() { t.Error("onWait called for a free repository") })
	if err != nil {
		t.Fatalf("WaitRepository: %v", err)
	}
	defer l.Unlock()
}

func TestWaitRepository_CancelledReturnsCause(t *testing.T) {
	shortenWait(t)
	dir := t.TempDir()

	held, ok, err := AcquireRepository(dir, "nas")
	if err != nil || !ok {
		t.Fatalf("setup acquire: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	cause := errors.New("interrupted by SIGTERM")
	ctx, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(20*time.Millisecond, func() { cancel(cause) })

	if _, err := WaitRepository(ctx, dir, "nas", nil); !errors.Is(err, cause) {
		t.Fatalf("got error %v, want the context's cause %v", err, cause)
	}
}

func TestWaitSlot_ReturnsOnceASlotFrees(t *testing.T) {
	shortenWait(t)
	dir := t.TempDir()

	held, ok, err := AcquireSlot(dir, 1)
	if err != nil || !ok {
		t.Fatalf("setup acquire: ok=%v err=%v", ok, err)
	}
	time.AfterFunc(30*time.Millisecond, func() { _ = held.Unlock() })

	l, err := WaitSlot(context.Background(), dir, 1, nil)
	if err != nil {
		t.Fatalf("WaitSlot: %v", err)
	}
	defer l.Unlock()
}

func TestWaitSlot_CancelledReturnsCause(t *testing.T) {
	shortenWait(t)
	dir := t.TempDir()

	held, ok, err := AcquireSlot(dir, 1)
	if err != nil || !ok {
		t.Fatalf("setup acquire: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	cause := errors.New("interrupted by SIGINT")
	ctx, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(20*time.Millisecond, func() { cancel(cause) })

	if _, err := WaitSlot(ctx, dir, 1, nil); !errors.Is(err, cause) {
		t.Fatalf("got error %v, want the context's cause %v", err, cause)
	}
}

func TestJobHeld_TrueWhileHeldFalseAfterUnlock(t *testing.T) {
	dir := t.TempDir()

	l, ok, err := AcquireJob(dir, "gitea")
	if err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}
	if held, err := JobHeld(dir, "gitea"); err != nil || !held {
		t.Fatalf("JobHeld while held: held=%v err=%v, want true", held, err)
	}

	if err := l.Unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if held, err := JobHeld(dir, "gitea"); err != nil || held {
		t.Fatalf("JobHeld after unlock: held=%v err=%v, want false", held, err)
	}
}

func TestJobHeld_MissingFileIsNotHeldAndCreatesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locks")

	if held, err := JobHeld(dir, "gitea"); err != nil || held {
		t.Fatalf("JobHeld with no lock file: held=%v err=%v, want false", held, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("JobHeld created the lock directory (stat err: %v)", err)
	}
}

// A probe leaves the lock free for the next execution.
func TestJobHeld_DoesNotKeepTheLock(t *testing.T) {
	dir := t.TempDir()

	l, ok, err := AcquireJob(dir, "gitea")
	if err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}
	l.Unlock()

	if _, err := JobHeld(dir, "gitea"); err != nil {
		t.Fatalf("JobHeld: %v", err)
	}
	l2, ok, err := AcquireJob(dir, "gitea")
	if err != nil || !ok {
		t.Fatalf("acquire after a probe: ok=%v err=%v", ok, err)
	}
	l2.Unlock()
}

// A status probe holds a shared lock for an instant; a job starting at
// that moment must still get its lock.
func TestAcquireJob_SucceedsWhenAProbeReleasesWithinTheRetryWindow(t *testing.T) {
	dir := t.TempDir()

	l, ok, err := AcquireJob(dir, "gitea")
	if err != nil || !ok {
		t.Fatalf("creating the lock file: ok=%v err=%v", ok, err)
	}
	l.Unlock()

	probe := flock.New(filepath.Join(dir, jobFile("gitea")))
	if got, err := probe.TryRLock(); err != nil || !got {
		t.Fatalf("probe lock: got=%v err=%v", got, err)
	}
	time.AfterFunc(50*time.Millisecond, func() { _ = probe.Unlock() })

	l2, ok, err := AcquireJob(dir, "gitea")
	if err != nil || !ok {
		t.Fatalf("acquire while a probe was in progress: ok=%v err=%v, want it to succeed", ok, err)
	}
	l2.Unlock()
}

func TestAcquireState_SerialisesHolders(t *testing.T) {
	dir := t.TempDir()

	l1, ok, err := AcquireState(dir)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	time.AfterFunc(30*time.Millisecond, func() { _ = l1.Unlock() })

	start := time.Now()
	l2, ok, err := AcquireState(dir)
	if err != nil || !ok {
		t.Fatalf("second acquire: ok=%v err=%v", ok, err)
	}
	defer l2.Unlock()
	if waited := time.Since(start); waited < 20*time.Millisecond {
		t.Fatalf("second acquire returned after %v, before the first holder released", waited)
	}
}
