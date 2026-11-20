package sync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"imgsync/discover"
	"imgsync/fetch"
	"imgsync/state"
)

func TestFindSeriesFolderConflict(t *testing.T) {
	base := t.TempDir()
	t.Setenv("IMGSYNC_OUTPUT", base)

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const slug = "some-series-abc123"
	const title = "Some Series"
	b := state.Bookmark{Slug: slug, Title: title}
	target := filepath.Join(base, title)
	page := filepath.Join(target, "Chapter 001", "001.webp")

	got, err := findSeriesFolderConflict(st, b)
	if err != nil || got != nil {
		t.Fatalf("empty output dir: conflict = %+v, err = %v", got, err)
	}

	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err = findSeriesFolderConflict(st, b)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.path != target || got.chapters != 1 || got.files != 0 {
		t.Fatalf("unowned folder: conflict = %+v", got)
	}

	// A page recorded inside the folder makes it ours, so no prompt is needed.
	if err := st.UpsertPage(slug, "001", 0, "url", page, "", "", "sha", 3); err != nil {
		t.Fatal(err)
	}
	got, err = findSeriesFolderConflict(st, b)
	if err != nil || got != nil {
		t.Fatalf("owned folder: conflict = %+v, err = %v", got, err)
	}

	// An explicit location is managed by set-location and is never questioned.
	b.Location = filepath.Join(base, "elsewhere")
	got, err = findSeriesFolderConflict(st, b)
	if err != nil || got != nil {
		t.Fatalf("explicit location: conflict = %+v, err = %v", got, err)
	}
}

func TestFolderOwnedByChapterRows(t *testing.T) {
	base := t.TempDir()
	t.Setenv("IMGSYNC_OUTPUT", base)

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const slug = "known-chapters"
	folder := filepath.Join(base, "Known Chapters")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "001.webp"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := state.Bookmark{Slug: slug, Title: "Known Chapters"}
	got, err := findSeriesFolderConflict(st, b)
	if err != nil || got == nil {
		t.Fatalf("before chapters: conflict = %+v, err = %v", got, err)
	}

	// unsync leaves chapter rows behind; the folder is still ours.
	if err := st.SetChapter(slug, "001", "url", state.ChapterStatusOK, 1); err != nil {
		t.Fatal(err)
	}
	got, err = findSeriesFolderConflict(st, b)
	if err != nil || got != nil {
		t.Fatalf("after chapters: conflict = %+v, err = %v", got, err)
	}
}

func TestFindSeriesFolderConflictLegacy(t *testing.T) {
	base := t.TempDir()
	t.Setenv("IMGSYNC_OUTPUT", base)

	st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const slug = "the-genius-knight-of-a-fallen-race-7ba78ec7"
	const title = "The Genius Knight of a Fallen Race"
	legacy := filepath.Join(base, discover.SeriesFolder(slug))
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "001.webp"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := findSeriesFolderConflict(st, state.Bookmark{Slug: slug, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.path != legacy || got.files != 1 {
		t.Fatalf("legacy folder: conflict = %+v", got)
	}
}

func TestConfirmFolderUse(t *testing.T) {
	c := &folderConflict{slug: "a", path: "/tmp/a", chapters: 2, files: 10}

	for _, tc := range []struct {
		name      string
		answer    string
		assumeYes bool
		want      bool
	}{
		{"yes", "y\n", false, true},
		{"yes long", "yes\n", false, true},
		{"no", "n\n", false, false},
		{"default", "\n", false, false},
		{"eof", "", false, false},
		{"assume yes", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := confirmFolderUse(c, tc.assumeYes, true, strings.NewReader(tc.answer))
			if err != nil || got != tc.want {
				t.Fatalf("confirmFolderUse = %v, %v; want %v, nil", got, err, tc.want)
			}
		})
	}

	if _, err := confirmFolderUse(c, false, false, strings.NewReader("y\n")); err == nil ||
		!strings.Contains(err.Error(), "--yes") {
		t.Fatalf("non-interactive error = %v, want --yes hint", err)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestSyncAllExistingSeriesFolder(t *testing.T) {
	disableProgress(t)
	base := t.TempDir()
	t.Setenv("IMGSYNC_OUTPUT", base)

	var imageRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/series/a"):
			_, _ = w.Write([]byte(`<a href="/chapter/1">Chapter 1</a>`))
		case strings.HasPrefix(r.URL.Path, "/chapter/"):
			_, _ = w.Write([]byte(`<img src="/img/1.png">`))
		default:
			imageRequests.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("new"))
		}
	}))
	defer srv.Close()

	run := func(t *testing.T, opts Options) ([]string, string) {
		t.Helper()
		st, err := state.Open(filepath.Join(t.TempDir(), "state.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		if _, err := st.UpsertBookmark("a", "id", srv.URL+"/series/a", "Series A"); err != nil {
			t.Fatal(err)
		}

		page := filepath.Join(base, "Series A", "Chapter 001", "001.png")
		if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(page, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}

		f := fetch.New()
		defer f.Close()
		_, failures, err := SyncAll(t.Context(), f, st, opts)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		return failures, string(data)
	}

	t.Run("non-interactive fails closed", func(t *testing.T) {
		imageRequests.Store(0)
		failures, data := run(t, Options{Input: strings.NewReader(""), Interactive: boolPtr(false)})
		if len(failures) != 1 || failures[0] != "a" {
			t.Fatalf("failures = %v, want [a]", failures)
		}
		if data != "old" {
			t.Fatalf("existing file = %q, want old", data)
		}
		if n := imageRequests.Load(); n != 0 {
			t.Fatalf("image requests = %d, want 0", n)
		}
	})

	t.Run("declined", func(t *testing.T) {
		imageRequests.Store(0)
		failures, data := run(t, Options{Input: strings.NewReader("n\n")})
		if len(failures) != 0 {
			t.Fatalf("failures = %v, want none", failures)
		}
		if data != "old" {
			t.Fatalf("existing file = %q, want old", data)
		}
		if n := imageRequests.Load(); n != 0 {
			t.Fatalf("image requests = %d, want 0", n)
		}
	})

	t.Run("confirmed", func(t *testing.T) {
		imageRequests.Store(0)
		failures, data := run(t, Options{AssumeYes: true})
		if len(failures) != 0 {
			t.Fatalf("failures = %v, want none", failures)
		}
		if data != "new" {
			t.Fatalf("existing file = %q, want new", data)
		}
		if n := imageRequests.Load(); n != 1 {
			t.Fatalf("image requests = %d, want 1", n)
		}
	})
}
