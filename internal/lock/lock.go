// Package lock provides cross-process coordination via plain OS file locks
// (flock), so that "tick" and "run" invocations - which are independent,
// one-shot processes with no daemon to coordinate through - can still
// safely enforce per-repository mutual exclusion and a global concurrency
// cap. flock is released by the kernel if a holding process crashes or is
// killed, so a stale lock from a dead process never needs separate cleanup
// logic.
package lock

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

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

// tryFile takes a non-blocking exclusive lock on name inside lockDir,
// creating the directory if needed. ok is false (with a nil error) if
// another process or lock currently holds it.
func tryFile(lockDir, name string) (l *Lock, ok bool, err error) {
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("creating lock dir %s: %w", lockDir, err)
	}
	path := filepath.Join(lockDir, name)
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

// waitInterval is how often a waiting acquisition retries. flock has no
// cancellable wait, so waiting is a polled retry. A variable so tests can
// shorten it.
var waitInterval = time.Second

// wait retries try every interval until it succeeds or ctx is cancelled, in
// which case it returns ctx's cause. onWait, if non-nil, is called once,
// when the first attempt finds the lock held.
func wait(ctx context.Context, interval time.Duration, try func() (*Lock, bool, error), onWait func()) (*Lock, error) {
	for waiting := false; ; waiting = true {
		l, ok, err := try()
		if err != nil {
			return nil, err
		}
		if ok {
			return l, nil
		}
		if !waiting && onWait != nil {
			onWait()
		}
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-time.After(interval):
		}
	}
}

// Timing for the short waits: a job lock that is only being probed by
// `status` (see JobHeld), and the state lock, which is held just long
// enough to rewrite one small file.
const (
	briefRetry     = 10 * time.Millisecond
	jobProbeWait   = 250 * time.Millisecond
	stateLockLimit = 5 * time.Second
)

// waitBriefly is wait with a deadline instead of a caller's context. ok is
// false (with a nil error) if the lock was still held when limit passed.
func waitBriefly(limit time.Duration, try func() (*Lock, bool, error)) (*Lock, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	l, err := wait(ctx, briefRetry, try, nil)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return l, true, nil
}

// AcquireJob takes a non-blocking exclusive lock scoped to a single job
// name, so that a job is never executed twice at once - by two ticks, a
// tick and a manual run, or two manual runs. It is taken before the job
// waits for anything else, so a job that is queued counts as held too. ok
// is false (with a nil error) if another execution of the job is running
// or waiting.
//
// It retries for a moment before giving up, because `status` briefly takes
// a shared lock on the same file to see whether the job is running, and
// that must not make a job look as if it is already running.
func AcquireJob(lockDir, jobName string) (l *Lock, ok bool, err error) {
	return waitBriefly(jobProbeWait, func() (*Lock, bool, error) { return tryFile(lockDir, jobFile(jobName)) })
}

func jobFile(jobName string) string { return "job-" + jobName + ".lock" }

// JobHeld reports whether an execution of the job currently holds its lock
// (see AcquireJob), meaning the job is running or waiting to start. It
// never creates the lock file, so it is safe for a read-only command run
// as any user: a job whose lock file doesn't exist has never run and is
// not held.
func JobHeld(lockDir, jobName string) (bool, error) {
	path := filepath.Join(lockDir, jobFile(jobName))
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checking %s: %w", path, err)
	}
	fl := flock.New(path)
	free, err := fl.TryRLock()
	if err != nil {
		return false, fmt.Errorf("locking %s: %w", path, err)
	}
	if !free {
		return true, nil
	}
	return false, fl.Unlock()
}

// AcquireState takes the exclusive lock that guards one read-modify-write
// of the state file, waiting a few seconds for another process to finish
// its own. ok is false (with a nil error) if it is still held after that.
func AcquireState(lockDir string) (l *Lock, ok bool, err error) {
	return waitBriefly(stateLockLimit, func() (*Lock, bool, error) { return tryFile(lockDir, "state.lock") })
}

// AcquireRepository takes a non-blocking exclusive lock scoped to a single
// repository name, so that no two processes - two ticks, a tick and a
// manual run, or two manual runs - ever run a restic operation against the
// same repository at the same time. ok is false (with a nil error) if
// another process currently holds the lock; exec refuses in that case,
// while jobs wait (see WaitRepository).
func AcquireRepository(lockDir, repoName string) (l *Lock, ok bool, err error) {
	return tryFile(lockDir, "repo-"+repoName+".lock")
}

// WaitRepository is AcquireRepository for a caller that would rather wait
// than be turned away: it retries until the lock is free or ctx is
// cancelled (returning ctx's cause). onWait is called once if it has to
// wait.
func WaitRepository(ctx context.Context, lockDir, repoName string, onWait func()) (*Lock, error) {
	return wait(ctx, waitInterval, func() (*Lock, bool, error) { return AcquireRepository(lockDir, repoName) }, onWait)
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

// WaitSlot is AcquireSlot for a caller that would rather wait than be
// turned away: it retries across every slot until one is free or ctx is
// cancelled (returning ctx's cause). onWait is called once if it has to
// wait.
func WaitSlot(ctx context.Context, lockDir string, maxConcurrent int, onWait func()) (*Lock, error) {
	return wait(ctx, waitInterval, func() (*Lock, bool, error) { return AcquireSlot(lockDir, maxConcurrent) }, onWait)
}
