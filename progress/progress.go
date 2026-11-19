// Package progress renders a single-line progress bar on stderr while a
// command runs. It stays out of the way when output is redirected: the bar is
// disabled whenever stderr is not a terminal or IMGSYNC_NO_PROGRESS is set.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	barWidth     = 24
	labelWidth   = 28
	redrawPeriod = 100 * time.Millisecond
)

var (
	mu      sync.Mutex
	enabled = detectEnabled()
	active  *Bar
)

func detectEnabled() bool {
	if os.Getenv("IMGSYNC_NO_PROGRESS") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// SetEnabled turns the bar on or off for the rest of the process. It is used
// by commands that expose a --no-progress flag.
func SetEnabled(v bool) {
	mu.Lock()
	defer mu.Unlock()
	enabled = v
	if !v && active != nil {
		fmt.Fprint(os.Stderr, "\r\x1b[K")
		active = nil
	}
}

// Bar is a live progress line. Methods are safe for concurrent use.
type Bar struct {
	label    string
	unit     string
	total    int
	done     int
	start    time.Time
	lastDraw time.Time
	finished bool
}

// New starts a bar that counts total items. A non-positive total disables the
// bar, which keeps "nothing to do" runs quiet.
func New(label, unit string, total int) *Bar {
	b := &Bar{label: label, unit: unit, total: total, start: time.Now()}
	mu.Lock()
	defer mu.Unlock()
	if enabled && total > 0 {
		active = b
		b.drawLocked()
	}
	return b
}

// Add advances the bar by n items.
func (b *Bar) Add(n int) {
	if n == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if !enabled || b.finished || active != b {
		return
	}
	b.done += n
	if b.done > b.total {
		b.done = b.total
	}
	if b.done < b.total && time.Since(b.lastDraw) < redrawPeriod {
		return
	}
	b.drawLocked()
}

// SetLabel replaces the label shown at the start of the line.
func (b *Bar) SetLabel(label string) {
	mu.Lock()
	defer mu.Unlock()
	b.label = label
	if enabled && active == b {
		b.drawLocked()
	}
}

// Finish renders the final state and releases the line for normal output.
func (b *Bar) Finish() {
	mu.Lock()
	defer mu.Unlock()
	if b.finished {
		return
	}
	b.finished = true
	if !enabled || active != b {
		return
	}
	fmt.Fprint(os.Stderr, "\r\x1b[K", b.line(), "\n")
	active = nil
}

func (b *Bar) drawLocked() {
	if !enabled || active != b {
		return
	}
	b.lastDraw = time.Now()
	fmt.Fprint(os.Stderr, "\r\x1b[K", b.line())
}

func (b *Bar) line() string {
	eta := "--"
	if d, ok := b.eta(); ok {
		eta = formatDuration(d)
	}
	rate := 0.0
	if elapsed := time.Since(b.start); elapsed > 0 {
		rate = float64(b.done) / elapsed.Seconds()
	}
	return barLine(b.label, b.unit, b.done, b.total, rate, eta)
}

func (b *Bar) eta() (time.Duration, bool) {
	if b.total <= 0 || b.done <= 0 {
		return 0, false
	}
	if b.done >= b.total {
		return 0, true
	}
	elapsed := time.Since(b.start)
	perItem := elapsed / time.Duration(b.done)
	return perItem * time.Duration(b.total-b.done), true
}

func barLine(label, unit string, done, total int, rate float64, eta string) string {
	if total < 0 {
		total = 0
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}

	pct := 100
	filled := 0
	if total > 0 {
		pct = done * 100 / total
		filled = done * barWidth / total
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	rateText := ""
	if done > 0 && rate > 0 {
		rateText = fmt.Sprintf("  %.1f/s", rate)
	}
	return fmt.Sprintf(
		"%-*s  [%s]  %3d%%  %d/%d %s%s  ETA %s",
		labelWidth, truncateLabel(label, labelWidth),
		bar, pct, done, total, unit, rateText, eta,
	)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int((d + time.Second - 1) / time.Second)
	if secs < 60 {
		return fmt.Sprintf("0:%02d", secs)
	}
	mins := secs / 60
	secs %= 60
	if mins < 60 {
		return fmt.Sprintf("%d:%02d", mins, secs)
	}
	hours := mins / 60
	mins %= 60
	return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
}

func truncateLabel(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

// Writer wraps an output writer (normally stderr) so log records clear the
// active bar, write their line and then redraw the bar.
func Writer(out io.Writer) io.Writer { return &barWriter{out: out} }

type barWriter struct {
	out io.Writer
}

func (w *barWriter) Write(p []byte) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	if !enabled || active == nil {
		return w.out.Write(p)
	}

	fmt.Fprint(w.out, "\r\x1b[K")
	n, err := w.out.Write(p)
	if err != nil {
		return n, err
	}
	if n > 0 && p[n-1] != '\n' {
		fmt.Fprint(w.out, "\n")
	}
	active.drawLocked()
	return n, nil
}
