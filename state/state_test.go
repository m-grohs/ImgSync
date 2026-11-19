package state

import (
	"path/filepath"
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
