package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/drewlsvern/rest-o-matic/internal/config"
	"github.com/drewlsvern/rest-o-matic/internal/execution"
	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// redrawInterval is the least time between two redraws of the terminal
// progress line for restic's reports; a change of phase redraws at once.
const redrawInterval = 250 * time.Millisecond

// fallbackTerminalWidth is the width the terminal line is cut to when the
// terminal's own can't be read. The line must never wrap: a carriage
// return only goes back to the start of the last row, so a wrapped line
// can't be redrawn in place.
const fallbackTerminalWidth = 80

// progressOut is the progress printer of the running `run` or `tick`, nil
// for every other command. Messages written while it is set go through
// its Println, so that they never land inside the terminal line.
var progressOut *progressPrinter

// progressPrinter reports the progress of the jobs this process runs, on
// stderr. On a terminal it keeps one line, redrawn in place, for all of
// them. Otherwise it writes complete lines, each with the time and a
// job's name: one at every change of phase, and one for each job in a
// backup every interval (none when interval is 0).
//
// It never writes an escape sequence, so it holds with --color=never: a
// redraw is a carriage return and the new text, padded with spaces over
// whatever was there.
type progressPrinter struct {
	w        io.Writer
	terminal bool
	// width is the terminal's width in columns, asked on each redraw.
	width    func() int
	interval time.Duration
	now      func() time.Time

	mu        sync.Mutex
	order     []string // jobs in the order they started
	jobs      map[string]*jobProgress
	drawn     int // columns the terminal line takes up now
	lastDrawn time.Time
}

type jobProgress struct {
	cur       state.Progress
	lastTimed time.Time
}

// startProgress sets up progressOut for this process's stderr, and
// returns it as the Progress to hand to each job.
func startProgress(cfg *config.Config) *progressPrinter {
	fd := int(os.Stderr.Fd())
	width := func() int {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			return w
		}
		return fallbackTerminalWidth
	}
	progressOut = newProgressPrinter(os.Stderr, term.IsTerminal(fd), width, cfg.ProgressLineInterval(), nil)
	return progressOut
}

func newProgressPrinter(w io.Writer, terminal bool, width func() int, interval time.Duration, now func() time.Time) *progressPrinter {
	if now == nil {
		now = time.Now
	}
	if width == nil {
		width = func() int { return fallbackTerminalWidth }
	}
	return &progressPrinter{w: w, terminal: terminal, width: width, interval: interval, now: now, jobs: map[string]*jobProgress{}}
}

// Phase records a job's change of phase, and shows it at once.
func (p *progressPrinter) Phase(job string, ph execution.Phase) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	jp, ok := p.jobs[job]
	if !ok {
		jp = &jobProgress{}
		p.jobs[job] = jp
		p.order = append(p.order, job)
	}
	jp.cur = execution.PhaseProgress(ph, now)
	jp.lastTimed = now
	if p.terminal {
		p.redraw(now)
		return
	}
	p.line(now, job, jp)
}

// BackupStatus records one of restic's reports for a job. On a terminal
// the line is redrawn, a few times a second at most; otherwise a line is
// written once an interval has passed since the job's last one.
func (p *progressPrinter) BackupStatus(job string, s execution.BackupStatus) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	jp, ok := p.jobs[job]
	if !ok {
		return
	}
	now := p.now()
	execution.ApplyStatus(&jp.cur, s, now)
	switch {
	case p.terminal:
		if now.Sub(p.lastDrawn) >= redrawInterval {
			p.redraw(now)
		}
	case p.interval > 0 && now.Sub(jp.lastTimed) >= p.interval:
		jp.lastTimed = now
		p.line(now, job, jp)
	}
}

// Retry records one of restic's retry messages for a job, and shows it at
// once: on a terminal by redrawing, otherwise as a line of its own.
func (p *progressPrinter) Retry(job, message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	jp, ok := p.jobs[job]
	if !ok {
		return
	}
	now := p.now()
	execution.ApplyRetry(&jp.cur, message, now)
	if p.terminal {
		p.redraw(now)
		return
	}
	p.line(now, job, jp)
}

// Done takes a finished job off the terminal line.
func (p *progressPrinter) Done(job string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.jobs[job]; !ok {
		return
	}
	delete(p.jobs, job)
	for i, name := range p.order {
		if name == job {
			p.order = append(p.order[:i], p.order[i+1:]...)
			break
		}
	}
	if p.terminal {
		p.redraw(p.now())
	}
}

// Println writes s and a newline to w, first clearing the terminal line
// if there is one. It is redrawn with the next report.
func (p *progressPrinter) Println(w io.Writer, s string) {
	if p == nil {
		fmt.Fprintln(w, s)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	fmt.Fprintln(w, s)
}

// Clear removes the terminal line, if there is one.
func (p *progressPrinter) Clear() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
}

// line writes one complete progress line. The caller holds p.mu.
func (p *progressPrinter) line(now time.Time, job string, jp *jobProgress) {
	fmt.Fprintf(p.w, "%s %s: %s\n", now.Format(time.RFC3339), job, progressSummary(*progressStatusOf(&jp.cur), now))
}

// redraw replaces the terminal line with every running job's progress.
// The caller holds p.mu.
func (p *progressPrinter) redraw(now time.Time) {
	p.lastDrawn = now
	parts := make([]string, 0, len(p.order))
	for _, job := range p.order {
		parts = append(parts, job+": "+progressSummary(*progressStatusOf(&p.jobs[job].cur), now))
	}
	text := truncate(strings.Join(parts, " | "), p.width()-1)
	n := utf8.RuneCountInString(text)
	pad := ""
	if p.drawn > n {
		pad = strings.Repeat(" ", p.drawn-n)
	}
	fmt.Fprint(p.w, "\r"+text+pad)
	if pad != "" {
		// Back to the end of the text, so the next redraw pads from there.
		fmt.Fprint(p.w, "\r"+text)
	}
	p.drawn = n
}

// clear blanks the terminal line and returns to its start. The caller
// holds p.mu.
func (p *progressPrinter) clear() {
	if !p.terminal || p.drawn == 0 {
		return
	}
	fmt.Fprint(p.w, "\r"+strings.Repeat(" ", p.drawn)+"\r")
	p.drawn = 0
}

// truncate cuts s to at most n characters, marking the cut with "...".
func truncate(s string, n int) string {
	if n < 4 {
		n = 4
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-3]) + "..."
}
