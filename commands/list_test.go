package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"imgsync/config"
	"imgsync/internal/ui"
	"imgsync/state"
)

func TestRenderListSummary(t *testing.T) {
	got := renderListSummary([]state.Bookmark{
		{ID: 2, Slug: "plain-slug"}, // fallback: "plain slug"
		{ID: 1, Slug: "hidden-slug", Title: "Visible Title", AssumeSyncedThrough: "344"},
	}, ui.Style{})

	for _, want := range []string{
		"#1", "Visible Title", "assumed through Chapter 344",
		"#2", "plain slug",
		"2 bookmarks · 'imgsync list <id>' for details",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary =\n%s\nwant %q", got, want)
		}
	}
	if strings.Contains(got, "hidden-slug") {
		t.Errorf("summary should prefer the title over the slug:\n%s", got)
	}
	if strings.Index(got, "#1") > strings.Index(got, "#2") {
		t.Errorf("summary should be ordered by ID:\n%s", got)
	}
	for _, unwanted := range []string{"URL", "Pages", "LOCATION", "Last run"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("summary should not contain %q:\n%s", unwanted, got)
		}
	}
}

func TestRenderListDetail(t *testing.T) {
	bookmark := state.Bookmark{
		ID:                  2,
		Slug:                "the-divine-demons-grand-ascension",
		SeriesID:            "abc123",
		URL:                 "https://site.example/comics/the-divine-demons-grand-ascension",
		Title:               "The Divine Demon's Grand Ascension",
		Location:            "~/Pictures/ImgSync/Divine",
		AssumeSyncedThrough: "074",
		Enabled:             true,
		AddedAt:             "2026-11-22T10:04:11Z",
		LastRun:             "2026-11-22T11:22:09Z",
		LastResult:          "ok",
		Chapters:            12,
		Pages:               291,
	}
	exceptions := map[string]bool{
		"104": true, "110": true, "111": true, "112": true,
		"113": true, "114": true, "115": true,
	}
	got := renderListDetail(bookmark, exceptions, ui.Style{})

	if !strings.Contains(got, "Bookmark #2 — The Divine Demon's Grand Ascension") {
		t.Errorf("detail headline missing:\n%s", got)
	}
	for _, row := range []struct{ label, value string }{
		{"Slug", "the-divine-demons-grand-ascension"},
		{"URL", "https://site.example/comics/the-divine-demons-grand-ascension"},
		{"Series ID", "abc123"},
		{"Status", "✓ enabled"},
		{"Assumed", "Chapter 074"},
		{"Exceptions", "Chapter 104, 110-115"},
		{"Chapters", "12"},
		{"Pages", "291"},
		{"Location", "~/Pictures/ImgSync/Divine"},
		{"Added", "2026-11-22T10:04:11Z"},
		{"Last run", "2026-11-22T11:22:09Z"},
		{"Last result", "✓ ok"},
	} {
		assertListRow(t, got, row.label, row.value)
	}
}

func assertListRow(t *testing.T, out, label, value string) {
	t.Helper()
	pattern := `(?m)^[ \t]+` + regexp.QuoteMeta(label) + `[ \t]+` + regexp.QuoteMeta(value) + `[ \t]*$`
	if !regexp.MustCompile(pattern).MatchString(out) {
		t.Errorf("detail missing row %q = %q:\n%s", label, value, out)
	}
}

func TestListCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	store, err := state.Open(config.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.UpsertBookmark("alpha-abc123", "abc123", "https://site.example/comics/alpha-abc123", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertBookmark("beta-def456", "def456", "https://site.example/comics/beta-def456", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAssumeSyncedThrough("alpha-abc123", "127"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddAssumeException("alpha-abc123", "104"); err != nil {
		t.Fatal(err)
	}

	code, out := captureListStdout(t, func() int { return List(nil) })
	if code != 0 {
		t.Fatalf("List() = %d, want 0", code)
	}
	for _, want := range []string{"#1", "Alpha", "assumed through Chapter 127", "#2", "beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("List() output =\n%s\nwant %q", out, want)
		}
	}

	code, out = captureListStdout(t, func() int { return List([]string{"1"}) })
	if code != 0 {
		t.Fatalf("List(1) = %d, want 0", code)
	}
	for _, want := range []string{"Bookmark #1", "Alpha", "Slug", "alpha-abc123", "Exceptions", "Chapter 104"} {
		if !strings.Contains(out, want) {
			t.Errorf("List(1) output =\n%s\nwant %q", out, want)
		}
	}

	code, _ = captureListStdout(t, func() int { return List([]string{"missing"}) })
	if code != 1 {
		t.Errorf("List(missing) = %d, want 1", code)
	}
}

func captureListStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	defer func() {
		os.Stdout = old
		_ = f.Close()
	}()

	code := fn()
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(data)
}
