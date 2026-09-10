// Package lock provides cross-process coordination via plain OS file locks
// (flock), so that "tick" and "run" invocations - which are independent,
// one-shot processes with no daemon to coordinate through - can still
// safely enforce per-repository mutual exclusion and a global concurrency
// cap. flock is released by the kernel if a holding process crashes or is
// killed, so a stale lock from a dead process never needs separate cleanup
// logic.
package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// Lock is a held, non-blocking file lock. Call Unlock when done with it.
type Lock struct {
	fl *flock.Flock
}

// Unlock releases the lock.
func (l *Lock) Unlock() error {
	if l == nil || l.fl == nil {
		return nil
	}
	return l.fl.Unlock()
}

// AcquireRepository takes a non-blocking exclusive lock scoped to a single
// repository name, so that no two processes - two ticks, a tick and a
// manual run, or two manual runs - ever run a restic operation against the
// same repository at the same time. ok is false (with a nil error) if
// another process currently holds the lock; the caller should defer the
// job rather than treat that as a failure.
func AcquireRepository(lockDir, repoName string) (l *Lock, ok bool, err error) {
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("creating lock dir %s: %w", lockDir, err)
	}
	path := filepath.Join(lockDir, "repo-"+repoName+".lock")
	fl := flock.New(path)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, false, fmt.Errorf("locking %s: %w", path, err)
	}
	if !locked {
		return nil, false, nil
	}
	return &Lock{fl: fl}, true, nil
}

// AcquireSlot takes a non-blocking exclusive lock on the first available of
// maxConcurrent pre-defined "slot" files, implementing a counting semaphore
// that works across independent processes (not just goroutines within one
// process). ok is false (with a nil error) if every slot is currently held
// by some other execution.
func AcquireSlot(lockDir string, maxConcurrent int) (l *Lock, ok bool, err error) {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("creating lock dir %s: %w", lockDir, err)
	}
	for i := 0; i < maxConcurrent; i++ {
		path := filepath.Join(lockDir, fmt.Sprintf("slot-%d.lock", i))
		fl := flock.New(path)
		locked, err := fl.TryLock()
		if err != nil {
			return nil, false, fmt.Errorf("locking %s: %w", path, err)
		}
		if locked {
			return &Lock{fl: fl}, true, nil
		}
	}
	return nil, false, nil
}
