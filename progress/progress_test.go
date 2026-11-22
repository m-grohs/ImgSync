package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoProgressEnvParsing(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"true", true},
		{"0", false},
		{"false", false},
		{"", false},
		{"nonsense", false},
	} {
		t.Setenv("IMGSYNC_NO_PROGRESS", tc.value)
		if got := noProgressSet(); got != tc.want {
			t.Errorf("noProgressSet(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestBarLine(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	line := barLine("series", "Chapter 005", "pages", 5, 10, 2.5, "~5s")
	for _, want := range []string{
		"series · Chapter 005",
		"[████████████░░░░░░░░░░░░]",
		" 50%",
		"5/10 pages",
		"2.5/s",
		"~5s",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("barLine() = %q, missing %q", line, want)
		}
	}
}

func TestBarLineLayout(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	index := barLine("Indexing", "", "series", 3, 8, 1.5, "~4s")
	if !strings.Contains(index, "Indexing  3/8 series  [") {
		t.Errorf("index line has unexpected layout: %q", index)
	}
	chapter := barLine("Blade & Soul", "Chapter 012", "pages", 8, 20, 3.1, "~4s")
	if !strings.Contains(chapter, "Blade & Soul · Chapter 012") {
		t.Errorf("chapter line is missing its labels: %q", chapter)
	}
	if strings.Index(chapter, "8/20 pages") > strings.Index(chapter, "[") {
		t.Errorf("counts should precede the bar: %q", chapter)
	}
	indexChapters := barLine("Indexing", "Blade & Soul", "chapters", 12, 137, 1.5, "~1m 23s")
	if !strings.Contains(indexChapters, "Indexing · Blade & Soul  12/137 chapters  [") {
		t.Errorf("chapter-based indexing line has unexpected layout: %q", indexChapters)
	}
}

func TestFinishedBarsStack(t *testing.T) {
	var buf bytes.Buffer
	mu.Lock()
	wasEnabled, wasOut, wasActive := enabled, out, active
	enabled, out, active = true, &buf, nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		enabled, out, active = wasEnabled, wasOut, wasActive
		mu.Unlock()
	})

	first := New("Series", "Chapter 001", "pages", 2)
	first.Finish()
	second := New("Series", "Chapter 002", "pages", 1)
	second.Finish()

	text := strings.ReplaceAll(buf.String(), "\r\x1b[K", "")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("finished bars produced %d lines, want 2: %q", len(lines), text)
	}
	if !strings.Contains(lines[0], "Chapter 001") || !strings.Contains(lines[1], "Chapter 002") {
		t.Errorf("chapter lines are not stacked in order: %q", lines)
	}
}

func TestBarLineBeforeFirstItem(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	line := barLine("series", "", "pages", 0, 10, 0, "")
	for _, want := range []string{"  0%", "0/10 pages"} {
		if !strings.Contains(line, want) {
			t.Errorf("barLine() = %q, missing %q", line, want)
		}
	}
	if strings.Contains(line, "/s") || strings.Contains(line, "ETA") || strings.Contains(line, "~") {
		t.Errorf("barLine() = %q, should not show a rate or ETA before any progress", line)
	}
}

func TestBarSetTotal(t *testing.T) {
	var buf bytes.Buffer
	mu.Lock()
	wasEnabled, wasOut, wasActive := enabled, out, active
	enabled, out, active = true, &buf, nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		enabled, out, active = wasEnabled, wasOut, wasActive
		mu.Unlock()
	})

	bar := New("Indexing", "", "chapters", 10)
	bar.Add(2)
	bar.SetTotal(4)
	text := strings.ReplaceAll(buf.String(), "\r\x1b[K", "")
	if !strings.Contains(text, "2/4 chapters") || !strings.Contains(text, " 50%") {
		t.Errorf("after shrinking total: %q", text)
	}

	bar.SetTotal(1)
	text = strings.ReplaceAll(buf.String(), "\r\x1b[K", "")
	if !strings.Contains(text, "1/1 chapters") || !strings.Contains(text, "100%") {
		t.Errorf("after clamping done: %q", text)
	}

	bar.SetTotal(8)
	text = strings.ReplaceAll(buf.String(), "\r\x1b[K", "")
	if !strings.Contains(text, "1/8 chapters") || !strings.Contains(text, " 12%") {
		t.Errorf("after growing total: %q", text)
	}
	bar.Finish()
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

func TestClearRemovesLine(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	var buf bytes.Buffer
	mu.Lock()
	wasEnabled, wasOut, wasActive := enabled, out, active
	enabled, out, active = true, &buf, nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		enabled, out, active = wasEnabled, wasOut, wasActive
		mu.Unlock()
	})

	bar := New("Series", "Chapter 001", "pages", 2)
	bar.Add(1)
	bar.Clear()
	text := strings.ReplaceAll(buf.String(), "\r\x1b[K", "")
	if strings.Contains(text, "Chapter 001\n") {
		t.Errorf("Clear() left a finished line behind: %q", text)
	}
	mu.Lock()
	stillActive := active == bar
	mu.Unlock()
	if stillActive {
		t.Error("Clear() did not release the active bar")
	}
}

func TestBarLineNarrowTerminal(t *testing.T) {
	t.Setenv("COLUMNS", "65")
	line := barLine("A Very Long Series Name", "Chapter 012", "pages", 1, 10, 1.0, "~9s")
	if runeLen(line) > 65 {
		t.Errorf("barLine() = %q, length %d, want <= 65", line, runeLen(line))
	}
}
