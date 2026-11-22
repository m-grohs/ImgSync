package state

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestBookmarkAndLocation(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	created, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "X Title")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}

	again, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "")
	if err != nil || again {
		t.Fatalf("expected re-enable, created=%v err=%v", again, err)
	}

	if err := st.SetLocation("x-abc123", "/tmp/x"); err != nil {
		t.Fatal(err)
	}
	b, err := st.GetBookmark("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || b.Location != "/tmp/x" || b.Title != "X Title" || !b.Enabled {
		t.Fatalf("bookmark = %+v", b)
	}

	missing, err := st.GetBookmark("nope")
	if err != nil || missing != nil {
		t.Fatalf("missing = %+v err = %v", missing, err)
	}
}

func TestIntegrateAndUnsync(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "X"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureChapter("x-abc123", "001", 2); err != nil {
		t.Fatal(err)
	}

	inserted, err := st.IntegratePage("x-abc123", "001", 0, "/tmp/001.webp", "sha", 10)
	if err != nil || !inserted {
		t.Fatalf("inserted=%v err=%v", inserted, err)
	}
	again, err := st.IntegratePage("x-abc123", "001", 0, "/tmp/001.webp", "sha", 10)
	if err != nil || again {
		t.Fatalf("expected no second insert, inserted=%v err=%v", again, err)
	}

	page, err := st.GetPage("x-abc123", "001", 0)
	if err != nil || page == nil || page.Path != "/tmp/001.webp" || page.Status != "ok" {
		t.Fatalf("page = %+v err = %v", page, err)
	}

	if err := st.SetPagePath("x-abc123", "001", 0, "/tmp/moved/001.webp"); err != nil {
		t.Fatal(err)
	}
	page, err = st.GetPage("x-abc123", "001", 0)
	if err != nil || page == nil || page.Path != "/tmp/moved/001.webp" {
		t.Fatalf("page after relink = %+v err = %v", page, err)
	}

	chapters, err := st.AllChaptersForBookmark("x-abc123")
	if err != nil || len(chapters) != 1 || chapters[0] != "001" {
		t.Fatalf("chapters = %v err = %v", chapters, err)
	}

	count, err := st.CountPagesForChapters("x-abc123", []string{"001"})
	if err != nil || count != 1 {
		t.Fatalf("count = %d err = %v", count, err)
	}

	deleted, err := st.UnsyncChapters("x-abc123", []string{"001"})
	if err != nil || deleted != 1 {
		t.Fatalf("deleted = %d err = %v", deleted, err)
	}
	page, err = st.GetPage("x-abc123", "001", 0)
	if err != nil || page != nil {
		t.Fatalf("page after unsync = %+v err = %v", page, err)
	}
}

func TestSyncedChaptersAndUnsyncReset(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "X"); err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"001", "002"} {
		if err := st.SetChapter("x-abc123", number, "url", ChapterStatusSynced, 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetChapter("x-abc123", "003", "url", ChapterStatusOK, 2); err != nil {
		t.Fatal(err)
	}

	synced, err := st.SyncedChapters("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !synced["001"] || !synced["002"] || synced["003"] || len(synced) != 2 {
		t.Fatalf("synced chapters = %v", synced)
	}

	if _, err := st.UnsyncChapters("x-abc123", []string{"001"}); err != nil {
		t.Fatal(err)
	}
	synced, err = st.SyncedChapters("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if synced["001"] || !synced["002"] || len(synced) != 1 {
		t.Fatalf("synced chapters after unsync = %v", synced)
	}
}

func TestAssumeSynced(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "X"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssumeSyncedThrough("x-abc123", "123"); err != nil {
		t.Fatal(err)
	}
	b, err := st.GetBookmark("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || b.AssumeSyncedThrough != "123" {
		t.Fatalf("bookmark = %+v", b)
	}

	if err := st.AddAssumeException("x-abc123", "050"); err != nil {
		t.Fatal(err)
	}
	exceptions, err := st.AssumeExceptions("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !exceptions["050"] || len(exceptions) != 1 {
		t.Fatalf("exceptions = %v", exceptions)
	}

	if err := st.ClearAssumeSynced("x-abc123"); err != nil {
		t.Fatal(err)
	}
	b, err = st.GetBookmark("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || b.AssumeSyncedThrough != "" {
		t.Fatalf("bookmark after clear = %+v", b)
	}
	exceptions, err = st.AssumeExceptions("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(exceptions) != 0 {
		t.Fatalf("exceptions after clear = %v", exceptions)
	}
}

func TestAddAssumeExceptions(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "X"); err != nil {
		t.Fatal(err)
	}
	for _, chapters := range [][]string{
		{"100", "101", "102", "100"},
		{"101", "104"},
		nil,
	} {
		if err := st.AddAssumeExceptions("x-abc123", chapters); err != nil {
			t.Fatal(err)
		}
	}
	exceptions, err := st.AssumeExceptions("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(exceptions) != 4 {
		t.Fatalf("exceptions = %v, want 4 chapters", exceptions)
	}
	for _, chapter := range []string{"100", "101", "102", "104"} {
		if !exceptions[chapter] {
			t.Errorf("missing exception for %s: %v", chapter, exceptions)
		}
	}
}

func TestOpenMigratesAssumeColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE bookmarks (
		slug        TEXT PRIMARY KEY,
		series_id   TEXT,
		url         TEXT NOT NULL,
		title       TEXT,
		location    TEXT,
		enabled     INTEGER NOT NULL DEFAULT 1,
		added_at    TEXT NOT NULL,
		last_run    TEXT,
		last_result TEXT
	);`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "X"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssumeSyncedThrough("x-abc123", "007"); err != nil {
		t.Fatal(err)
	}
	b, err := st.GetBookmark("x-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || b.AssumeSyncedThrough != "007" {
		t.Fatalf("bookmark = %+v", b)
	}
}

func TestBookmarkIDsAndResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, slug := range []string{
		"blade-of-the-discarded-7ba78ec7",
		"blade-runner-12345678",
		"solo-leveling-99999999",
	} {
		if _, err := st.UpsertBookmark(slug, "id", "https://site/comics/"+slug, slug); err != nil {
			t.Fatal(err)
		}
	}

	solo, err := st.GetBookmark("solo-leveling-99999999")
	if err != nil || solo == nil || solo.ID == 0 {
		t.Fatalf("bookmark = %+v err = %v", solo, err)
	}

	// Exact slug, numeric ID and unique prefix all resolve to the same row.
	bySlug, err := st.ResolveBookmark("solo-leveling-99999999")
	if err != nil || bySlug == nil || bySlug.ID != solo.ID {
		t.Fatalf("by slug = %+v err = %v", bySlug, err)
	}
	byID, err := st.ResolveBookmark(strconv.FormatInt(solo.ID, 10))
	if err != nil || byID == nil || byID.Slug != solo.Slug {
		t.Fatalf("by id = %+v err = %v", byID, err)
	}
	byPrefix, err := st.ResolveBookmark("solo")
	if err != nil || byPrefix == nil || byPrefix.Slug != solo.Slug {
		t.Fatalf("by prefix = %+v err = %v", byPrefix, err)
	}

	// An ambiguous prefix is rejected and names the candidates.
	if _, err := st.ResolveBookmark("blade"); err == nil {
		t.Fatal("expected ambiguous prefix error")
	}
	if missing, err := st.ResolveBookmark("nothing-like-this"); err != nil || missing != nil {
		t.Fatalf("missing = %+v err = %v", missing, err)
	}

	// IDs stay stable across reopen.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.GetBookmark(solo.Slug)
	if err != nil || again == nil || again.ID != solo.ID {
		t.Fatalf("after reopen = %+v err = %v", again, err)
	}

	// Removing a bookmark also removes its ID.
	if removed, err := st.RemoveBookmark(solo.Slug); err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, err := st.BookmarkID(solo.Slug); err != sql.ErrNoRows {
		t.Fatalf("expected no id after removal, err = %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenBackfillsBookmarkIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE bookmarks (
		slug        TEXT PRIMARY KEY,
		series_id   TEXT,
		url         TEXT NOT NULL,
		title       TEXT,
		location    TEXT,
		assume_synced_through TEXT NOT NULL DEFAULT '',
		enabled     INTEGER NOT NULL DEFAULT 1,
		added_at    TEXT NOT NULL,
		last_run    TEXT,
		last_result TEXT
	);`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO bookmarks(slug, url, enabled, added_at) VALUES('x-abc123','https://site/comics/x-abc123',1,'2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	b, err := st.GetBookmark("x-abc123")
	if err != nil || b == nil || b.ID == 0 {
		t.Fatalf("bookmark = %+v err = %v", b, err)
	}
	resolved, err := st.ResolveBookmark(strconv.FormatInt(b.ID, 10))
	if err != nil || resolved == nil || resolved.Slug != "x-abc123" {
		t.Fatalf("resolved = %+v err = %v", resolved, err)
	}
}

func TestRemoveBookmarkCleansBookkeepingRows(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("x-abc123", "abc", "https://site/x-abc123", "X"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAssumeException("x-abc123", "001"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordRun("x-abc123", "now", "now", 1, 0, 0, 0, "note"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPage("x-abc123", "001", 0, "url", "/tmp/001.webp", "", "", "sha", 1); err != nil {
		t.Fatal(err)
	}
	if removed, err := st.RemoveBookmark("x-abc123"); err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}

	var runs int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("runs = %d, err = %v", runs, err)
	}
	// Page rows are kept on purpose: re-adding the series must not re-download.
	pages, err := st.PagesForBookmark("x-abc123")
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
}

func TestUpsertPage(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("x-abc123", "abc123", "https://site/comics/x-abc123", "X"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPage("x-abc123", "001", 0, "https://cdn/001.webp", "/tmp/001.webp", "etag", "lm", "sha", 42); err != nil {
		t.Fatal(err)
	}
	page, err := st.GetPage("x-abc123", "001", 0)
	if err != nil || page == nil {
		t.Fatalf("page = %+v err = %v", page, err)
	}
	if page.URL != "https://cdn/001.webp" || page.ETag != "etag" || page.Size != 42 {
		t.Fatalf("page = %+v", page)
	}
}

func TestResetStartsBookmarkIDsAtOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"a", "b"} {
		if _, err := st.UpsertBookmark(slug, "", "https://site/comics/"+slug, slug); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// WAL databases can leave sidecar files behind; all of them must go.
	sidecars := []string{path + "-wal", path + "-shm", path + "-journal"}
	for _, p := range sidecars {
		if err := os.WriteFile(p, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(filepath.Dir(path), "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Reset(path); err != nil {
		t.Fatal(err)
	}
	for _, p := range sidecars {
		content, err := os.ReadFile(p)
		if err == nil && string(content) == "stale" {
			t.Fatalf("stale sidecar %s survived reset", p)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("reading %s: %v", p, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	bookmarks, err := st.ListBookmarks()
	if err != nil || len(bookmarks) != 0 {
		t.Fatalf("bookmarks after reset = %d, err = %v", len(bookmarks), err)
	}
	if _, err := st.UpsertBookmark("new", "", "https://site/comics/new", "new"); err != nil {
		t.Fatal(err)
	}
	id, err := st.BookmarkID("new")
	if err != nil || id != 1 {
		t.Fatalf("id after reset = %d, err = %v", id, err)
	}
}

func TestResetWithoutExistingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	if err := Reset(path); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	bookmarks, err := st.ListBookmarks()
	if err != nil || len(bookmarks) != 0 {
		t.Fatalf("bookmarks = %d, err = %v", len(bookmarks), err)
	}
}
