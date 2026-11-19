package sync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"imgsync/discover"
	"imgsync/fetch"
	"imgsync/progress"
	"imgsync/state"
)

func TestPendingPages(t *testing.T) {
	pages := []discover.Page{{Index: 0}, {Index: 1}, {Index: 2}}

	if got := pendingPages(pages, nil); got != 3 {
		t.Errorf("pendingPages(nil) = %d, want 3", got)
	}
	if got := pendingPages(pages, map[int]bool{1: true}); got != 2 {
		t.Errorf("pendingPages({1}) = %d, want 2", got)
	}
	if got := pendingPages(pages, map[int]bool{0: true, 1: true, 2: true}); got != 0 {
		t.Errorf("pendingPages(all) = %d, want 0", got)
	}
}

func TestSyncSeriesIncremental(t *testing.T) {
	progress.SetEnabled(false)

	var imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/series/test":
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a><a href="/chapter/2">Chapter 2</a>`))
		case "/chapter/1":
			_, _ = w.Write([]byte(`<img src="/img/1-1.png"><img src="/img/1-2.png">`))
		case "/chapter/2":
			_, _ = w.Write([]byte(`<img src="/img/2-1.png"><img src="/img/2-2.png">`))
		default:
			imageRequests.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("img"))
		}
	}))
	defer srv.Close()

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	f := fetch.New()
	defer f.Close()

	bookmark := state.Bookmark{Slug: "test", URL: srv.URL + "/series/test"}
	out := t.TempDir()

	summary, err := SyncSeries(f, st, bookmark, out, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 4 || summary.Skipped != 0 || summary.Failed != 0 || summary.Chapters != 2 {
		t.Fatalf("first run summary = %+v", summary)
	}
	if got := imageRequests.Load(); got != 4 {
		t.Fatalf("image requests = %d, want 4", got)
	}
	for _, name := range []string{"001.png", "002.png"} {
		if _, err := os.Stat(filepath.Join(out, "Chapter 001", name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}

	summary, err = SyncSeries(f, st, bookmark, out, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 0 || summary.Skipped != 4 || summary.Chapters != 2 {
		t.Fatalf("second run summary = %+v", summary)
	}
	if got := imageRequests.Load(); got != 4 {
		t.Fatalf("image requests after second run = %d, want 4", got)
	}
}

func TestSeriesOutRootUsesTitle(t *testing.T) {
	base := t.TempDir()
	t.Setenv("IMGSYNC_OUTPUT", base)

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	got := seriesOutRoot(st, state.Bookmark{Slug: "some-series-abc123", Title: "Some Series"}, false)
	want := filepath.Join(base, "Some Series")
	if got != want {
		t.Fatalf("seriesOutRoot = %q, want %q", got, want)
	}
}

func TestSeriesOutRootRenamesLegacyFolder(t *testing.T) {
	base := t.TempDir()
	t.Setenv("IMGSYNC_OUTPUT", base)

	const slug = "the-genius-knight-of-a-fallen-race-7ba78ec7"
	const title = "The Genius Knight of a Fallen Race"

	legacy := filepath.Join(base, discover.SeriesFolder(slug))
	oldPage := filepath.Join(legacy, "chapter 001", "001.webp")
	if err := os.MkdirAll(filepath.Dir(oldPage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPage, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertPage(slug, "001", 0, "url", oldPage, "", "", "sha", 3); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(base, title)
	if got := seriesOutRoot(st, state.Bookmark{Slug: slug, Title: title}, true); got != legacy {
		t.Fatalf("dry-run seriesOutRoot = %q, want legacy folder %q", got, legacy)
	}
	if !isDir(legacy) {
		t.Fatal("dry run must not rename the folder")
	}

	if got := seriesOutRoot(st, state.Bookmark{Slug: slug, Title: title}, false); got != want {
		t.Fatalf("seriesOutRoot = %q, want %q", got, want)
	}
	if !isDir(want) {
		t.Fatalf("expected %s to exist", want)
	}
	if isDir(legacy) {
		t.Fatalf("legacy folder %s still exists", legacy)
	}

	pages, err := st.PagesForBookmark(slug)
	if err != nil {
		t.Fatal(err)
	}
	newPage := filepath.Join(want, "chapter 001", "001.webp")
	if len(pages) != 1 || pages[0].Path != newPage {
		t.Fatalf("pages = %+v, want path %q", pages, newPage)
	}
}

func TestNormalizeChapterFolders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Series")
	const slug = "series-abc123"

	oldDir := filepath.Join(root, "chapter 001")
	oldPage := filepath.Join(oldDir, "001.webp")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPage, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, "extras")
	if err := os.MkdirAll(unrelated, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertPage(slug, "001", 0, "url", oldPage, "", "", "sha", 3); err != nil {
		t.Fatal(err)
	}

	if err := normalizeChapterFolders(st, slug, root, true); err != nil {
		t.Fatal(err)
	}
	if !isDir(oldDir) {
		t.Fatal("dry run must not rename chapter folders")
	}

	if err := normalizeChapterFolders(st, slug, root, false); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(root, "Chapter 001")
	if !isDir(newDir) {
		t.Fatalf("expected %s to exist", newDir)
	}
	if isDir(oldDir) {
		t.Fatalf("legacy folder %s still exists", oldDir)
	}
	if !isDir(unrelated) {
		t.Fatal("unrelated folder was renamed")
	}

	pages, err := st.PagesForBookmark(slug)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(newDir, "001.webp"); len(pages) != 1 || pages[0].Path != want {
		t.Fatalf("pages = %+v, want path %q", pages, want)
	}

	if err := normalizeChapterFolders(st, slug, root, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

func TestSyncSeriesLimit(t *testing.T) {
	progress.SetEnabled(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/series/test":
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a><a href="/chapter/2">Chapter 2</a>`))
		case "/chapter/1", "/chapter/2":
			_, _ = w.Write([]byte(`<img src="/img/1.png"><img src="/img/2.png">`))
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("img"))
		}
	}))
	defer srv.Close()

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	f := fetch.New()
	defer f.Close()

	limit := 1
	summary, err := SyncSeries(
		f, st, state.Bookmark{Slug: "test", URL: srv.URL + "/series/test"}, t.TempDir(),
		false, false, &limit,
	)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 1 || summary.Chapters != 1 {
		t.Fatalf("limit summary = %+v, want 1 new page in 1 chapter", summary)
	}
}
