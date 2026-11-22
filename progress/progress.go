// Package progress renders a single-line progress bar on stderr while a
// command runs. It stays out of the way when output is redirected: the bar is
// disabled whenever stderr is not a terminal or IMGSYNC_NO_PROGRESS is set.
package progress

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"imgsync/internal/ui"
)

const (
	maxBarWidth  = 24
	redrawPeriod = 100 * time.Millisecond
	defaultWidth = 90
)

var (
	mu      sync.Mutex
	enabled = detectEnabled()
	active  *Bar
	out     io.Writer = os.Stderr
)

func detectEnabled() bool {
	if noProgressSet() || os.Getenv("TERM") == "dumb" {
		return false
	}
	return ui.IsTerminal(os.Stderr)
}

// noProgressSet reports whether IMGSYNC_NO_PROGRESS is explicitly true.
// Values like "0" and "false" deliberately keep progress enabled.
func noProgressSet() bool {
	v, err := strconv.ParseBool(os.Getenv("IMGSYNC_NO_PROGRESS"))
	return err == nil && v
}

// Enabled reports whether bars are currently rendered. Tests use it to save
// and restore global state.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// SetEnabled turns the bar on or off for the rest of the process. It is used
// by commands that expose a --no-progress flag.
func SetEnabled(v bool) {
	mu.Lock()
	defer mu.Unlock()
	enabled = v
	if !v && active != nil {
		fmt.Fprint(out, "\r\x1b[K")
		active = nil
	}
}

// Bar is a live progress line. Methods are safe for concurrent use.
type Bar struct {
	label    string
	detail   string
	unit     string
	total    int
	done     int
	start    time.Time
	lastDraw time.Time
	finished bool
}

// New starts a bar that counts total items. label and detail are the two
// columns at the start of the line; an empty detail leaves the line compact.
// A non-positive total disables the bar, which keeps "nothing to do" runs quiet.
func New(label, detail, unit string, total int) *Bar {
	b := &Bar{label: label, detail: detail, unit: unit, total: total, start: time.Now()}
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

// SetTotal changes the number of items the bar counts. It is used when the
// amount of work becomes known, or shrinks, after the bar was created.
func (b *Bar) SetTotal(total int) {
	mu.Lock()
	defer mu.Unlock()
	b.total = total
	if b.done > b.total {
		b.done = b.total
	}
	if enabled && active == b {
		b.drawLocked()
	}
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

// SetDetail replaces the secondary label (for example the current chapter)
// shown after the primary label.
func (b *Bar) SetDetail(detail string) {
	mu.Lock()
	defer mu.Unlock()
	b.detail = detail
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
	fmt.Fprint(out, "\r\x1b[K", b.lineLocked(), "\n")
	active = nil
}

// Clear removes a transient bar without leaving a line behind. Indexing
// phases and per-series download bars use it so only the live bar is visible.
func (b *Bar) Clear() {
	mu.Lock()
	defer mu.Unlock()
	if b.finished {
		return
	}
	b.finished = true
	if !enabled || active != b {
		return
	}
	fmt.Fprint(out, "\r\x1b[K")
	active = nil
}

func (b *Bar) drawLocked() {
	if !enabled || active != b {
		return
	}
	b.lastDraw = time.Now()
	fmt.Fprint(out, "\r\x1b[K", b.lineLocked())
}

func (b *Bar) lineLocked() string {
	eta := ""
	if d, ok := b.eta(); ok && b.done < b.total {
		eta = "~" + ui.Duration(d)
	}
	rate := 0.0
	if elapsed := time.Since(b.start); elapsed > 0 {
		rate = float64(b.done) / elapsed.Seconds()
	}
	return barLineStyled(b.label, b.detail, b.unit, b.done, b.total, rate, eta,
		ui.Style{Enabled: ui.ColorsEnabled(out)})
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

func barLine(label, detail, unit string, done, total int, rate float64, eta string) string {
	return barLineStyled(label, detail, unit, done, total, rate, eta, ui.Style{})
}

func barLineStyled(label, detail, unit string, done, total int, rate float64, eta string, st ui.Style) string {
	if total < 0 {
		total = 0
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}

	width := terminalWidth()
	barWidth := maxBarWidth
	switch {
	case width < 70:
		barWidth = 10
	case width < 80:
		barWidth = 16
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
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"

	rateText := ""
	if done > 0 && rate > 0 {
		rateText = fmt.Sprintf("  %.1f/s", rate)
	}
	etaText := ""
	if eta != "" {
		etaText = "  " + eta
	}
	counts := fmt.Sprintf("%d/%d %s", done, total, unit)
	suffix := fmt.Sprintf("  %s  %s  %3d%%%s%s", counts, bar, pct, rateText, etaText)
	prefix := fitLabel(label, detail, width-runeLen(suffix))

	return st.Bold(prefix) +
		"  " + counts +
		"  " + st.Dim("[") + st.BarFill(strings.Repeat("█", filled)) +
		st.Dim(strings.Repeat("░", barWidth-filled)) + st.Dim("]") +
		fmt.Sprintf("  %s", st.Bold(fmt.Sprintf("%3d%%", pct))) +
		st.Dim(rateText) + st.Dim(etaText)
}

func terminalWidth() int {
	if raw := os.Getenv("COLUMNS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 60 && n <= 300 {
			return n
		}
	}
	return defaultWidth
}

// fitLabel keeps the series name and, when there is room, the detail on one
// line. The series name wins when space is tight.
func fitLabel(label, detail string, max int) string {
	if max <= 0 {
		return ""
	}
	if detail == "" {
		return truncateLabel(label, max)
	}
	const sep = " " + ui.Dot + " "
	if runeLen(label)+runeLen(sep)+runeLen(detail) <= max {
		return label + sep + detail
	}
	detailMax := max / 2
	if detailMax > 16 {
		detailMax = 16
	}
	if detailMax < 1 {
		return truncateLabel(label, max)
	}
	shortDetail := truncateLabel(detail, detailMax)
	labelMax := max - runeLen(sep) - runeLen(shortDetail)
	if labelMax < 4 {
		return truncateLabel(label, max)
	}
	return truncateLabel(label, labelMax) + sep + shortDetail
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

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
func Writer(w io.Writer) io.Writer { return &barWriter{out: w} }

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
