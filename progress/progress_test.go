package progress

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{500 * time.Millisecond, "0:01"},
		{5 * time.Second, "0:05"},
		{59 * time.Second, "0:59"},
		{60 * time.Second, "1:00"},
		{65 * time.Second, "1:05"},
		{59*time.Minute + 59*time.Second, "59:59"},
		{time.Hour, "1:00:00"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1:02:03"},
	}
	for _, tc := range cases {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestBarLine(t *testing.T) {
	line := barLine("series", "Chapter 005", "pages", 5, 10, 2.5, "0:05")
	for _, want := range []string{
		"series",
		"[████████████░░░░░░░░░░░░]",
		" 50%",
		"5/10 pages",
		"2.5/s",
		"ETA 0:05",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("barLine() = %q, missing %q", line, want)
		}
	}
	if !strings.Contains(line, "Chapter 005") {
		t.Errorf("barLine() = %q, missing detail", line)
	}
}

func TestBarLineBeforeFirstItem(t *testing.T) {
	line := barLine("series", "", "pages", 0, 10, 0, "--")
	for _, want := range []string{"  0%", "0/10 pages", "ETA --"} {
		if !strings.Contains(line, want) {
			t.Errorf("barLine() = %q, missing %q", line, want)
		}
	}
	if strings.Contains(line, "/s") {
		t.Errorf("barLine() = %q, should not show a rate before any progress", line)
	}
}

func TestBarLineClampsOvershoot(t *testing.T) {
	line := barLine("series", "", "pages", 20, 10, 0, "0:00")
	for _, want := range []string{"100%", "10/10 pages"} {
		if !strings.Contains(line, want) {
			t.Errorf("barLine() = %q, missing %q", line, want)
		}
	}
}

func TestTruncateLabel(t *testing.T) {
	if got := truncateLabel("short", 10); got != "short" {
		t.Errorf("truncateLabel(short) = %q, want unchanged", got)
	}
	got := truncateLabel("a very long series label", 10)
	if runes := []rune(got); len(runes) != 10 {
		t.Errorf("truncateLabel() = %q, length %d, want 10", got, len(runes))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncateLabel() = %q, want ellipsis suffix", got)
	}
}
