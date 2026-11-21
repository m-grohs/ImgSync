package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imgsync/config"
	"imgsync/state"
)

func setupImportState(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	location := filepath.Join(t.TempDir(), "existing-series")
	if err := os.MkdirAll(location, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(config.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.UpsertBookmark("alpha-abc123", "abc123", "https://site.example/comics/alpha-abc123", "Old title"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLocation("alpha-abc123", location); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssumeSyncedThrough("alpha-abc123", "050"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAssumeException("alpha-abc123", "010"); err != nil {
		t.Fatal(err)
	}
	return config.DBPath(), location
}

func openState(t *testing.T) *state.State {
	t.Helper()
	st, err := state.Open(config.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func writeImportFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportMergeAndUpdate(t *testing.T) {
	_, oldLocation := setupImportState(t)
	path := writeImportFile(t, "bookmarks.txt", `https://site.example/comics/alpha-abc123 assume=127 title="New title" location="/definitely/missing/path" enabled=0
https://site.example/comics/beta-def456 assume=003
`)

	if code := Import([]string{path}); code != 0 {
		t.Fatalf("Import() = %d", code)
	}
	st := openState(t)
	alpha, err := st.GetBookmark("alpha-abc123")
	if err != nil || alpha == nil {
		t.Fatalf("alpha = %+v, err = %v", alpha, err)
	}
	if alpha.AssumeSyncedThrough != "050" || alpha.Title != "Old title" || alpha.Location != oldLocation || !alpha.Enabled {
		t.Fatalf("merge should leave existing fields alone: %+v", alpha)
	}
	exceptions, err := st.AssumeExceptions("alpha-abc123")
	if err != nil || !exceptions["010"] {
		t.Fatalf("exceptions = %v, err = %v", exceptions, err)
	}
	beta, err := st.GetBookmark("beta-def456")
	if err != nil || beta == nil || beta.AssumeSyncedThrough != "003" || !beta.Enabled {
		t.Fatalf("beta = %+v, err = %v", beta, err)
	}
	if beta.Location != "" {
		t.Fatalf("new bookmark got a location from a missing path: %q", beta.Location)
	}

	if code := Import([]string{path, "--update"}); code != 0 {
		t.Fatalf("Import --update = %d", code)
	}
	alpha, err = st.GetBookmark("alpha-abc123")
	if err != nil || alpha == nil {
		t.Fatalf("alpha = %+v, err = %v", alpha, err)
	}
	if alpha.AssumeSyncedThrough != "127" || alpha.Title != "New title" || alpha.Enabled {
		t.Fatalf("--update should apply file fields: %+v", alpha)
	}
	if alpha.Location != oldLocation {
		t.Fatalf("--update should keep the existing location when the imported one is missing: %q", alpha.Location)
	}
	exceptions, err = st.AssumeExceptions("alpha-abc123")
	if err != nil || len(exceptions) != 1 || !exceptions["010"] {
		t.Fatalf("exceptions after update = %v, err = %v", exceptions, err)
	}
}

func TestImportValidLocationAndClearing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "series")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeImportFile(t, "bookmarks.json", `{
  "bookmarks": [
    {"url": "https://site.example/comics/alpha-abc123", "assume": "127", "except": ["104", "110-111"], "location": "`+strings.ReplaceAll(dir, `\`, `\\`)+`"}
  ]
}`)
	if code := Import([]string{path}); code != 0 {
		t.Fatalf("Import() = %d", code)
	}
	st := openState(t)
	b, err := st.GetBookmark("alpha-abc123")
	if err != nil || b == nil {
		t.Fatalf("bookmark = %+v, err = %v", b, err)
	}
	if b.Location != dir {
		t.Fatalf("location = %q, want %q", b.Location, dir)
	}
	exceptions, err := st.AssumeExceptions("alpha-abc123")
	if err != nil || len(exceptions) != 3 || !exceptions["104"] || !exceptions["110"] || !exceptions["111"] {
		t.Fatalf("exceptions = %v, err = %v", exceptions, err)
	}

	clearPath := writeImportFile(t, "clear.csv", "url,assume,except,title,location,enabled\nhttps://site.example/comics/alpha-abc123,none,none,none,none,1\n")
	if code := Import([]string{clearPath, "--update"}); code != 0 {
		t.Fatalf("Import clear = %d", code)
	}
	b, err = st.GetBookmark("alpha-abc123")
	if err != nil || b == nil {
		t.Fatalf("bookmark = %+v, err = %v", b, err)
	}
	if b.AssumeSyncedThrough != "" || b.Location != "" || b.Title != "" {
		t.Fatalf("clear fields = %+v", b)
	}
	exceptions, err = st.AssumeExceptions("alpha-abc123")
	if err != nil || len(exceptions) != 0 {
		t.Fatalf("exceptions after clear = %v, err = %v", exceptions, err)
	}
}

func TestImportIgnoresFileAsLocation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := writeImportFile(t, "bookmarks.txt", "https://site.example/comics/eta-pqr678 location=\""+file+"\"\n")
	if code := Import([]string{path}); code != 0 {
		t.Fatalf("Import() = %d", code)
	}
	st := openState(t)
	b, err := st.GetBookmark("eta-pqr678")
	if err != nil || b == nil {
		t.Fatalf("bookmark = %+v, err = %v", b, err)
	}
	if b.Location != "" {
		t.Fatalf("file path was stored as location: %q", b.Location)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	_, _ = setupImportState(t)
	path := writeImportFile(t, "bookmarks.txt", `https://site.example/comics/alpha-abc123 assume=999
https://site.example/comics/delta-ghi789 assume=011
`)
	if code := Import([]string{path, "--dry-run", "--update"}); code != 0 {
		t.Fatalf("Import --dry-run = %d", code)
	}
	st := openState(t)
	alpha, err := st.GetBookmark("alpha-abc123")
	if err != nil || alpha == nil || alpha.AssumeSyncedThrough != "050" {
		t.Fatalf("alpha changed during dry run: %+v, err = %v", alpha, err)
	}
	delta, err := st.GetBookmark("delta-ghi789")
	if err != nil || delta != nil {
		t.Fatalf("delta created during dry run: %+v, err = %v", delta, err)
	}
}

func TestImportInvalidFileWritesNothing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path := writeImportFile(t, "bookmarks.txt", `https://site.example/comics/epsilon-jkl012 assume=011
https://example.com/not-a-series assume=012
`)
	if code := Import([]string{path}); code != 2 {
		t.Fatalf("Import invalid = %d, want 2", code)
	}
	st := openState(t)
	bookmarks, err := st.ListBookmarks()
	if err != nil || len(bookmarks) != 0 {
		t.Fatalf("bookmarks = %+v, err = %v", bookmarks, err)
	}
}

func TestImportFormatOverride(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path := writeImportFile(t, "bookmarks.data", `{"bookmarks":[{"url":"https://site.example/comics/zeta-mno345","assume":"021"}]}`)
	if code := Import([]string{path, "--format", "json"}); code != 0 {
		t.Fatalf("Import --format json = %d", code)
	}
	st := openState(t)
	zeta, err := st.GetBookmark("zeta-mno345")
	if err != nil || zeta == nil || zeta.AssumeSyncedThrough != "021" {
		t.Fatalf("zeta = %+v, err = %v", zeta, err)
	}
}

func TestExportFormats(t *testing.T) {
	setupImportState(t)
	for _, format := range []string{formatTxt, formatCSV, formatJSON} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bookmarks."+format)
			if code := Export([]string{"-o", path}); code != 0 {
				t.Fatalf("Export() = %d", code)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			entries, _, err := decodeBookmarks(data, format)
			if err != nil {
				t.Fatalf("exported file is not valid %s: %v\n%s", format, err, data)
			}
			if len(entries) != 1 || entries[0].Slug != "alpha-abc123" {
				t.Fatalf("entries = %+v", entries)
			}
			if !equalPtr(entries[0].Assume, ptr("050")) || !equalPtr(entries[0].Title, ptr("Old title")) {
				t.Fatalf("exported fields = %+v", entries[0])
			}
			if entries[0].Except == nil || len(*entries[0].Except) != 1 || (*entries[0].Except)[0] != "010" {
				t.Fatalf("exported exceptions = %v", entries[0].Except)
			}
			if format == formatJSON && !strings.Contains(string(data), `"format": "imgsync-bookmarks"`) {
				t.Fatalf("json export lacks format marker: %s", data)
			}
		})
	}
}
