package lock

import "testing"

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
