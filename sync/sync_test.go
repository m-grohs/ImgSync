package sync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"
	"sync/atomic"
	"testing"

	"imgsync/discover"
	"imgsync/fetch"
	"imgsync/progress"
	"imgsync/state"
)

func disableProgress(t *testing.T) {
	t.Helper()
	was := progress.Enabled()
	progress.SetEnabled(false)
	t.Cleanup(func() { progress.SetEnabled(was) })
}

func TestDisplayName(t *testing.T) {
	if got := displayName(state.Bookmark{Title: "Blade and Soul", Slug: "blade"}); got != "Blade and Soul" {
		t.Errorf("displayName(title) = %q, want title", got)
	}
	if got := displayName(state.Bookmark{Slug: "blade"}); got != "blade" {
		t.Errorf("displayName(no title) = %q, want slug", got)
	}
}

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

func TestSummaryText(t *testing.T) {
	s := Summary{
		New:         71,
		Assumed:     121,
		Chapters:    125,
		NewChapters: []string{"122", "123", "124", "125"},
	}
	if got, want := s.Note(), "+4 chapters (Chapter 122-125), 71 pages, 121 chapters assumed"; got != want {
		t.Errorf("Note() = %q, want %q", got, want)
	}
	if got, want := s.Text(), "+4 chapters (71 pages), 121 chapters assumed"; got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}

	empty := Summary{Assumed: 121, Synced: 4, Chapters: 125}
	if got, want := empty.Note(), "no new pages, 121 chapters assumed, 4 chapters up to date"; got != want {
		t.Errorf("empty Note() = %q, want %q", got, want)
	}

	failed := Summary{New: 1, NewChapters: []string{"125"}, Skipped: 2, Failed: 3, Locked: 1}
	if got, want := failed.Note(), "+1 chapter (Chapter 125), 1 page, 2 pages unchanged, 1 locked, 3 failed"; got != want {
		t.Errorf("failed Note() = %q, want %q", got, want)
	}
}

func TestSyncSeriesIncremental(t *testing.T) {
	disableProgress(t)

	var chapterRequests, imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/series/test":
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a><a href="/chapter/2">Chapter 2</a>`))
		case "/chapter/1":
			chapterRequests.Add(1)
			_, _ = w.Write([]byte(`<img src="/img/1-1.png"><img src="/img/1-2.png">`))
		case "/chapter/2":
			chapterRequests.Add(1)
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

	summary, err := SyncSeries(t.Context(), f, st, bookmark, out, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 4 || summary.Skipped != 0 || summary.Synced != 0 || summary.Failed != 0 ||
		summary.Chapters != 2 || len(summary.NewChapters) != 2 {
		t.Fatalf("first run summary = %+v", summary)
	}
	if got := imageRequests.Load(); got != 4 {
		t.Fatalf("image requests = %d, want 4", got)
	}
	if got := chapterRequests.Load(); got != 2 {
		t.Fatalf("chapter requests = %d, want 2", got)
	}
	for _, name := range []string{"001.png", "002.png"} {
		if _, err := os.Stat(filepath.Join(out, "Chapter 001", name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}

	summary, err = SyncSeries(t.Context(), f, st, bookmark, out, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 0 || summary.Skipped != 0 || summary.Synced != 2 ||
		summary.Chapters != 2 || len(summary.NewChapters) != 0 {
		t.Fatalf("second run summary = %+v", summary)
	}
	if got := imageRequests.Load(); got != 4 {
		t.Fatalf("image requests after second run = %d, want 4", got)
	}
	if got := chapterRequests.Load(); got != 2 {
		t.Fatalf("chapter requests after second run = %d, want 2 (synced chapters must be skipped)", got)
	}
}

func TestSyncSeriesUnsyncReindexesChapter(t *testing.T) {
	disableProgress(t)

	var chapterRequests, imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/series/test":
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a><a href="/chapter/2">Chapter 2</a>`))
		case "/chapter/1", "/chapter/2":
			chapterRequests.Add(1)
			_, _ = w.Write([]byte(`<img src="/img/` + r.URL.Path[len("/chapter/"):] + `.png">`))
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

	if _, err := SyncSeries(t.Context(), f, st, bookmark, out, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UnsyncChapters("test", []string{"001"}); err != nil {
		t.Fatal(err)
	}

	summary, err := SyncSeries(t.Context(), f, st, bookmark, out, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 1 || summary.Synced != 1 || len(summary.NewChapters) != 1 || summary.NewChapters[0] != "001" {
		t.Fatalf("summary after unsync = %+v", summary)
	}
	if got := chapterRequests.Load(); got != 3 {
		t.Fatalf("chapter requests = %d, want 3 (chapter 1 re-indexed, chapter 2 skipped)", got)
	}
	if got := imageRequests.Load(); got != 3 {
		t.Fatalf("image requests = %d, want 3 (only the unsynced chapter re-downloads)", got)
	}
}

func TestSyncSeriesAssumeSyncedThrough(t *testing.T) {
	disableProgress(t)

	var chapterRequests, imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/series/test":
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a><a href="/chapter/2">Chapter 2</a><a href="/chapter/3">Chapter 3</a>`))
		case "/chapter/1", "/chapter/2":
			chapterRequests.Add(1)
			_, _ = w.Write([]byte(`<img src="/img/old.png">`))
		case "/chapter/3":
			chapterRequests.Add(1)
			_, _ = w.Write([]byte(`<img src="/img/3-1.png"><img src="/img/3-2.png">`))
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

	bookmark := state.Bookmark{
		Slug:                "test",
		URL:                 srv.URL + "/series/test",
		AssumeSyncedThrough: "002",
	}
	summary, err := SyncSeries(t.Context(), f, st, bookmark, t.TempDir(), false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Assumed != 2 || summary.Chapters != 3 || summary.New != 2 || summary.Skipped != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := chapterRequests.Load(); got != 1 {
		t.Fatalf("chapter requests = %d, want 1 (only chapter 3)", got)
	}
	if got := imageRequests.Load(); got != 2 {
		t.Fatalf("image requests = %d, want 2", got)
	}
}

func TestSyncSeriesAssumeException(t *testing.T) {
	disableProgress(t)

	var chapterRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/series/test":
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a><a href="/chapter/2">Chapter 2</a>`))
		case "/chapter/1", "/chapter/2":
			chapterRequests.Add(1)
			_, _ = w.Write([]byte(`<img src="/img/` + r.URL.Path[len("/chapter/"):] + `.png">`))
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

	bookmark := state.Bookmark{
		Slug:                "test",
		URL:                 srv.URL + "/series/test",
		AssumeSyncedThrough: "002",
	}
	if err := st.AddAssumeException("test", "001"); err != nil {
		t.Fatal(err)
	}

	summary, err := SyncSeries(t.Context(), f, st, bookmark, t.TempDir(), false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Assumed != 1 || summary.Chapters != 2 || summary.New != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := chapterRequests.Load(); got != 1 {
		t.Fatalf("chapter requests = %d, want 1", got)
	}
	row, err := st.GetPage("test", "001", 0)
	if err != nil {
		t.Fatal(err)
	}
	if row == nil {
		t.Fatal("chapter 001 was not downloaded despite the exception")
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

	got, err := seriesOutRoot(st, state.Bookmark{Slug: "some-series-abc123", Title: "Some Series"}, false)
	if err != nil {
		t.Fatal(err)
	}
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
	dryRoot, err := seriesOutRoot(st, state.Bookmark{Slug: slug, Title: title}, true)
	if err != nil {
		t.Fatal(err)
	}
	if dryRoot != legacy {
		t.Fatalf("dry-run seriesOutRoot = %q, want legacy folder %q", dryRoot, legacy)
	}
	if !isDir(legacy) {
		t.Fatal("dry run must not rename the folder")
	}

	got, err := seriesOutRoot(st, state.Bookmark{Slug: slug, Title: title}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
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

func TestSyncAllLimitIsPerBookmark(t *testing.T) {
	disableProgress(t)
	base := t.TempDir()
	t.Setenv("IMGSYNC_OUTPUT", base)

	var imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/series/a"), strings.HasSuffix(r.URL.Path, "/series/b"):
			_, _ = w.Write([]byte(`<a href="/chapter/a1">Chapter 1</a><a href="/chapter/a2">Chapter 2</a>`))
		case strings.HasPrefix(r.URL.Path, "/chapter/"):
			_, _ = w.Write([]byte(`<img src="/img/` + strings.TrimPrefix(r.URL.Path, "/chapter/") + `.png">`))
		default:
			imageRequests.Add(1)
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte("img"))
		}
	}))
	defer srv.Close()

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, b := range []struct{ slug, url string }{
		{"a", srv.URL + "/series/a"},
		{"b", srv.URL + "/series/b"},
	} {
		if _, err := st.UpsertBookmark(b.slug, "id", b.url, b.slug); err != nil {
			t.Fatal(err)
		}
	}
	f := fetch.New()
	defer f.Close()

	limit := 1
	totals, failures, err := SyncAll(t.Context(), f, st, nil, false, false, &limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 || totals.New != 2 {
		t.Fatalf("totals = %+v, failures = %v", totals, failures)
	}
	for _, slug := range []string{"a", "b"} {
		pages, err := st.PagesForBookmark(slug)
		if err != nil || len(pages) != 1 {
			t.Fatalf("%s pages = %+v, err = %v", slug, pages, err)
		}
	}
	if got := imageRequests.Load(); got != 2 {
		t.Fatalf("image requests = %d, want 2", got)
	}
}

func TestSyncAllIndexesAllSeriesBeforeDownloading(t *testing.T) {
	disableProgress(t)
	t.Setenv("IMGSYNC_OUTPUT", t.TempDir())

	var mu stdsync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/series/a"), strings.HasSuffix(r.URL.Path, "/series/b"):
			record("series")
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a>`))
		case strings.HasPrefix(r.URL.Path, "/chapter/"):
			record("chapter")
			_, _ = w.Write([]byte(`<img src="/img/page.png">`))
		default:
			record("image")
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
	for _, slug := range []string{"a", "b"} {
		if _, err := st.UpsertBookmark(slug, "id", srv.URL+"/series/"+slug, slug); err != nil {
			t.Fatal(err)
		}
	}
	f := fetch.New()
	defer f.Close()

	if _, failures, err := SyncAll(t.Context(), f, st, nil, false, false, nil); err != nil || len(failures) != 0 {
		t.Fatalf("SyncAll: failures = %v, err = %v", failures, err)
	}

	mu.Lock()
	defer mu.Unlock()
	lastIndex, firstDownload := -1, len(events)
	for i, event := range events {
		if event == "series" || event == "chapter" {
			lastIndex = i
		}
		if event == "image" && i < firstDownload {
			firstDownload = i
		}
	}
	if lastIndex == -1 || firstDownload == len(events) {
		t.Fatalf("incomplete request log: %v", events)
	}
	if lastIndex > firstDownload {
		t.Errorf("indexing and downloads interleaved: %v", events)
	}
}

func TestVerifySkipsPagesIntegratedFromDisk(t *testing.T) {
	disableProgress(t)
	var imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/series/test" {
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a>`))
			return
		}
		if r.URL.Path == "/chapter/1" {
			_, _ = w.Write([]byte(`<img src="/img/1.png">`))
			return
		}
		imageRequests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("img"))
	}))
	defer srv.Close()

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.EnsureChapter("test", "001", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.IntegratePage("test", "001", 0, "/somewhere/001.png", "sha", 3); err != nil {
		t.Fatal(err)
	}
	f := fetch.New()
	defer f.Close()

	summary, err := SyncSeries(t.Context(), f, st,
		state.Bookmark{Slug: "test", URL: srv.URL + "/series/test"}, t.TempDir(), false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 0 || summary.Failed != 0 || summary.Skipped != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := imageRequests.Load(); got != 0 {
		t.Fatalf("image requests = %d, want 0", got)
	}
}

func TestVerifyUnchangedBytesCountAsSkipped(t *testing.T) {
	disableProgress(t)
	var imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/series/test" {
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a>`))
			return
		}
		if r.URL.Path == "/chapter/1" {
			_, _ = w.Write([]byte(`<img src="/img/1.png">`))
			return
		}
		imageRequests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("img"))
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

	if _, err := SyncSeries(t.Context(), f, st, bookmark, out, false, false, nil); err != nil {
		t.Fatal(err)
	}
	summary, err := SyncSeries(t.Context(), f, st, bookmark, out, false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 0 || summary.Skipped != 1 || summary.Failed != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := imageRequests.Load(); got != 2 {
		t.Fatalf("image requests = %d, want 2", got)
	}
	page, err := st.GetPage("test", "001", 0)
	if err != nil || page == nil || page.ETag != `"v1"` {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestDryRunVerifySendsConditionalGet(t *testing.T) {
	disableProgress(t)
	var conditionalGets atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/series/test" {
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a>`))
			return
		}
		if r.URL.Path == "/chapter/1" {
			_, _ = w.Write([]byte(`<img src="/img/1.png">`))
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditionalGets.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("img"))
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

	if _, err := SyncSeries(t.Context(), f, st, bookmark, out, false, false, nil); err != nil {
		t.Fatal(err)
	}
	summary, err := SyncSeries(t.Context(), f, st, bookmark, out, true, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 0 || summary.Skipped != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := conditionalGets.Load(); got != 1 {
		t.Fatalf("conditional GETs = %d, want 1", got)
	}
}

func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "001.webp")
	for _, data := range []string{"a", "b"} {
		if _, err := AtomicWrite(dest, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "001.webp" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestSupersededExtensionIsRemoved(t *testing.T) {
	disableProgress(t)
	var useWebp atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/series/test" {
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a>`))
			return
		}
		if r.URL.Path == "/chapter/1" {
			ext := "png"
			if useWebp.Load() {
				ext = "webp"
			}
			_, _ = w.Write([]byte(`<img src="/img/1.` + ext + `">`))
			return
		}
		w.Header().Set("Content-Type", "image/"+strings.TrimPrefix(filepath.Ext(r.URL.Path), "."))
		if useWebp.Load() {
			_, _ = w.Write([]byte("new bytes"))
		} else {
			_, _ = w.Write([]byte("old bytes"))
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

	if _, err := SyncSeries(t.Context(), f, st, bookmark, out, false, false, nil); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(out, "Chapter 001", "001.png")
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatal(err)
	}

	useWebp.Store(true)
	summary, err := SyncSeries(t.Context(), f, st, bookmark, out, false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still present, err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "Chapter 001", "001.webp")); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSeriesLimit(t *testing.T) {
	disableProgress(t)

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
		t.Context(), f, st, state.Bookmark{Slug: "test", URL: srv.URL + "/series/test"}, t.TempDir(),
		false, false, &limit,
	)
	if err != nil {
		t.Fatal(err)
	}
	if summary.New != 1 || summary.Chapters != 1 || summary.Synced != 0 {
		t.Fatalf("limit summary = %+v, want 1 new page in 1 chapter", summary)
	}
	synced, err := st.SyncedChapters("test")
	if err != nil {
		t.Fatal(err)
	}
	if synced["001"] {
		t.Fatal("a partially downloaded chapter must not be marked synced")
	}
}
