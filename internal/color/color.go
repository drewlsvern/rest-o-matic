// Package color colours rest-o-matic's own status labels by severity when a
// person is watching a terminal, and leaves output byte-for-byte unchanged
// otherwise.
package color

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

const (
	red    = "\x1b[31m"
	orange = "\x1b[38;5;208m"
	green  = "\x1b[32m"
	reset  = "\x1b[0m"
)

// Painter wraps text in a severity colour, or returns it unchanged when off.
type Painter struct{ on bool }

// NewPainter returns a Painter that colours text only when on is true.
func NewPainter(on bool) Painter { return Painter{on: on} }

func (p Painter) paint(code, s string) string {
	if !p.on {
		return s
	}
	return code + s + reset
}

// Error colours s red.
func (p Painter) Error(s string) string { return p.paint(red, s) }

// Warn colours s orange.
func (p Painter) Warn(s string) string { return p.paint(orange, s) }

// Success colours s green.
func (p Painter) Success(s string) string { return p.paint(green, s) }

// Stdout and Stderr colour text destined for the matching stream. Both are
// off until Configure runs, so anything printed earlier stays plain.
var (
	Stdout Painter
	Stderr Painter
)

// Decide reports whether colour should be on for one stream: an explicit
// "always" or "never" mode wins, then a non-empty NO_COLOR disables colour,
// and otherwise ("auto") colour follows whether the stream is a terminal.
func Decide(mode, noColor string, isTTY bool) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		if noColor != "" {
			return false, nil
		}
		return isTTY, nil
	}
	return false, fmt.Errorf("invalid --color value %q: must be auto, always, or never", mode)
}

// Configure sets Stdout and Stderr for this process from the --color mode,
// the NO_COLOR environment variable, and whether each stream is a terminal.
func Configure(mode string) error {
	noColor := os.Getenv("NO_COLOR")
	out, err := Decide(mode, noColor, term.IsTerminal(int(os.Stdout.Fd())))
	if err != nil {
		return err
	}
	errOn, _ := Decide(mode, noColor, term.IsTerminal(int(os.Stderr.Fd())))
	Stdout, Stderr = NewPainter(out), NewPainter(errOn)
	return nil
}
