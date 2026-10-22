package main

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/drewlsvern/rest-o-matic/internal/execution"
)

// testClock is a clock the test moves by hand.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// syncBuffer is a bytes.Buffer safe for writes from several goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func backupPhase(repo string) execution.Phase {
	return execution.Phase{Name: execution.PhaseBackup, Repository: repo, Index: 1, Count: 2}
}

func TestProgressPrinter_PlainLines(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	var out syncBuffer
	p := newProgressPrinter(&out, false, nil, 5*time.Minute, clock.now)

	p.Phase("config", execution.Phase{Name: execution.PhaseBeforeHooks})
	p.Phase("config", backupPhase("s3"))
	// 20 minutes of reports, one a second.
	for i := 1; i <= 20*60; i++ {
		clock.advance(time.Second)
		p.BackupStatus("config", execution.BackupStatus{BytesDone: int64(i) << 20, TotalBytes: 2400 << 20, PercentDone: float64(i) / 2400})
	}
	p.Phase("config", execution.Phase{Name: execution.PhaseForget, Repository: "s3", Index: 1, Count: 2})

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("got %d lines, want 2 phase lines, 4 timed lines and 1 more phase line:\n%s", len(lines), out.String())
	}
	want := []string{
		"2026-10-21T09:00:00Z config: before hooks",
		"2026-10-21T09:00:00Z config: backing up to s3 (1 of 2)",
		"2026-10-21T09:05:00Z config: backing up to s3 (1 of 2), 12%, 300.0 MiB of 2.3 GiB",
		"2026-10-21T09:20:00Z config: applying retention on s3 (1 of 2)",
	}
	for _, i := range []int{0, 1, 2, 6} {
		w := want[0]
		want = want[1:]
		if lines[i] != w {
			t.Errorf("line %d: got %q, want %q", i, lines[i], w)
		}
	}
	if strings.ContainsAny(out.String(), "\r\x1b") {
		t.Errorf("plain output holds a carriage return or escape: %q", out.String())
	}
}

func TestProgressPrinter_TimedLinesOff(t *testing.T) {
	clock := &testClock{t: time.Now()}
	var out syncBuffer
	p := newProgressPrinter(&out, false, nil, 0, clock.now)
	p.Phase("config", backupPhase("s3"))
	for i := 0; i < 3600; i++ {
		clock.advance(time.Second)
		p.BackupStatus("config", execution.BackupStatus{BytesDone: int64(i)})
	}
	if n := strings.Count(out.String(), "\n"); n != 1 {
		t.Fatalf("got %d lines with timed lines off, want only the phase line:\n%s", n, out.String())
	}
}

// Lines for jobs running at once never mix within a line.
func TestProgressPrinter_ConcurrentJobs(t *testing.T) {
	var out syncBuffer
	p := newProgressPrinter(&out, false, nil, 10*time.Second, nil)
	var wg sync.WaitGroup
	for j := 0; j < 4; j++ {
		job := "job" + strconv.Itoa(j)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				p.Phase(job, backupPhase("nas"))
			}
		}()
	}
	wg.Wait()
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		if strings.Count(line, ": backing up to nas (1 of 2)") != 1 || !strings.Contains(line, " job") {
			t.Fatalf("mixed or broken line: %q", line)
		}
	}
}

func TestProgressPrinter_TerminalLine(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	var out syncBuffer
	p := newProgressPrinter(&out, true, func() int { return 120 }, 5*time.Minute, clock.now)

	p.Phase("config", backupPhase("s3"))
	p.Phase("development", execution.Phase{Name: execution.PhaseBeforeHooks})
	for i := 1; i <= 40; i++ {
		clock.advance(100 * time.Millisecond)
		p.BackupStatus("config", execution.BackupStatus{BytesDone: int64(i) << 20, TotalBytes: 100 << 20, PercentDone: float64(i) / 100})
	}
	frames := strings.Split(out.String(), "\r")
	// 2 phase redraws, then at most one every 250ms over 4s.
	if redraws := len(frames) - 1; redraws > 2*(2+16) {
		t.Errorf("redrew %d times in 4s", redraws)
	}
	last := frames[len(frames)-1]
	// Redraws come at most every 250ms, so the line may be a report behind.
	if !strings.HasPrefix(last, "config: backing up to s3 (1 of 2), 39%") && !strings.HasPrefix(last, "config: backing up to s3 (1 of 2), 40%") ||
		!strings.Contains(last, " | development: before hooks") {
		t.Errorf("last frame %q", last)
	}
	for _, f := range frames {
		if utf8.RuneCountInString(f) > 119 {
			t.Errorf("frame wider than the terminal: %q", f)
		}
	}
	if strings.ContainsAny(out.String(), "\x1b\n") {
		t.Errorf("terminal line holds an escape or newline: %q", out.String())
	}

	// A message clears the line first, and the line ends up gone once
	// every job is done.
	var msg syncBuffer
	last = frames[len(frames)-1]
	p.Println(&msg, "job config: waiting for a free slot")
	if !strings.HasSuffix(out.String(), "\r"+strings.Repeat(" ", utf8.RuneCountInString(last))+"\r") {
		t.Errorf("line not cleared before a message: %q", out.String()[len(out.String())-80:])
	}
	p.Done("config")
	p.Done("development")
	frames = strings.Split(out.String(), "\r")
	if strings.TrimSpace(frames[len(frames)-1]) != "" {
		t.Errorf("line left after every job finished: %q", frames[len(frames)-1])
	}
}

func TestProgressPrinter_NilIsSafe(t *testing.T) {
	var p *progressPrinter
	p.Phase("x", backupPhase("nas"))
	p.BackupStatus("x", execution.BackupStatus{})
	p.Done("x")
	p.Clear()
	var b bytes.Buffer
	p.Println(&b, "hello")
	if b.String() != "hello\n" {
		t.Fatalf("got %q", b.String())
	}
}

func TestProgressPrinter_CutToTerminalWidth(t *testing.T) {
	var out syncBuffer
	p := newProgressPrinter(&out, true, func() int { return 30 }, 0, nil)
	p.Phase("a-job-with-a-long-name", backupPhase("a-repository-with-a-long-name"))
	frame := strings.TrimPrefix(out.String(), "\r")
	if utf8.RuneCountInString(frame) != 29 || !strings.HasSuffix(frame, "...") {
		t.Fatalf("got %q", frame)
	}
}

func TestProgressPrinter_RetryLines(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	var out syncBuffer
	p := newProgressPrinter(&out, false, nil, 5*time.Minute, clock.now)
	p.Phase("config", execution.Phase{Name: execution.PhaseCheck, Repository: "s3", Index: 1, Count: 2})
	for i := 0; i < 3; i++ {
		clock.advance(30 * time.Second)
		p.Retry("config", "dial tcp 192.0.2.1:443: i/o timeout")
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "2026-10-21T09:00:00Z config: checking s3 (1 of 2)" ||
		lines[3] != "2026-10-21T09:01:30Z config: checking s3 (1 of 2), restic is retrying: dial tcp 192.0.2.1:443: i/o timeout" {
		t.Fatalf("got:\n%s", out.String())
	}
}
