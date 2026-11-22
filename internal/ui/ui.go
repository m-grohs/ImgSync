// Package ui holds the small presentation helpers shared by the progress
// renderer, the run summary and help output.
package ui

import (
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

// Symbols used in terminal output.
const (
	Check  = "✓"
	Cross  = "✗"
	Warn   = "⚠"
	Dot    = "·"
	EmDash = "—"
)

// IsTerminal reports whether w writes to a terminal.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// ColorsEnabled reports whether ANSI colors should be used for w.
func ColorsEnabled(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(w)
}

// Style renders text with ANSI escapes when enabled.
type Style struct {
	Enabled bool
}

func (s Style) paint(code, text string) string {
	if !s.Enabled || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// Bold renders a section heading or headline.
func (s Style) Bold(text string) string { return s.paint("1", text) }

// Dim renders secondary information.
func (s Style) Dim(text string) string { return s.paint("2", text) }

// Success renders a green success headline.
func (s Style) Success(text string) string { return s.paint("1;32", text) }

// Error renders a red failure headline.
func (s Style) Error(text string) string { return s.paint("1;31", text) }

// Warning renders a yellow interruption headline.
func (s Style) Warning(text string) string { return s.paint("1;33", text) }

// BarFill renders the filled part of a progress bar.
func (s Style) BarFill(text string) string { return s.paint("32", text) }

// Duration formats d as a compact human duration, e.g. "42s", "1m 04s" or
// "1h 02m". Sub-second durations round up so an ETA never reads "0s" while
// work is still in progress.
func Duration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	secs := int(math.Ceil(d.Seconds()))
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	secs %= 60
	if mins < 60 {
		return fmt.Sprintf("%dm %02ds", mins, secs)
	}
	hours := mins / 60
	mins %= 60
	return fmt.Sprintf("%dh %02dm", hours, mins)
}
