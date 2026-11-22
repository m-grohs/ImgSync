package commands

import (
	"path/filepath"
	"strings"
	"testing"

	"imgsync/config"
	"imgsync/state"
)

func seedUnsyncState(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	path := config.DBPath()
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertBookmark("alpha-abc123", "abc123", "https://site.example/comics/alpha-abc123", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssumeSyncedThrough("alpha-abc123", "200"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPage("alpha-abc123", "150", 0, "https://cdn/150.webp", "/tmp/150.webp", "", "", "sha", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func unsyncState(t *testing.T, path string) *state.State {
	t.Helper()
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestUnsyncMultipleChapterRanges(t *testing.T) {
	path := seedUnsyncState(t)

	code, out := captureListStdout(t, func() int {
		return Unsync([]string{"alpha-abc123", "100-102", "104", "150", "201", "--dry-run"})
	})
	if code != 0 {
		t.Fatalf("dry-run Unsync() = %d, want 0", code)
	}
	if !strings.Contains(out, "Chapter 100-102, 104: assumed synced - would re-download next run") {
		t.Errorf("dry-run output missing assumed range:\n%s", out)
	}
	st := unsyncState(t, path)
	exceptions, err := st.AssumeExceptions("alpha-abc123")
	if err != nil || len(exceptions) != 0 {
		t.Fatalf("dry-run added exceptions: %v, err = %v", exceptions, err)
	}
	if page, err := st.GetPage("alpha-abc123", "150", 0); err != nil || page == nil {
		t.Fatalf("dry-run removed page rows: page = %+v, err = %v", page, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	code, out = captureListStdout(t, func() int {
		return Unsync([]string{"alpha-abc123", "100-102", "104", "150", "201"})
	})
	if code != 0 {
		t.Fatalf("Unsync() = %d, want 0", code)
	}
	for _, want := range []string{
		"Chapter 100-102, 104: assumed synced - will re-download next run",
		"Chapter 150: 1 page(s) unsynced (will re-download next run)",
		"Chapter 201: nothing synced",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	st = unsyncState(t, path)
	defer st.Close()
	exceptions, err = st.AssumeExceptions("alpha-abc123")
	if err != nil {
		t.Fatal(err)
	}
	for _, chapter := range []string{"100", "101", "102", "104", "150"} {
		if !exceptions[chapter] {
			t.Errorf("missing exception for %s: %v", chapter, exceptions)
		}
	}
	if exceptions["201"] || len(exceptions) != 5 {
		t.Fatalf("unexpected exceptions: %v", exceptions)
	}
	if page, err := st.GetPage("alpha-abc123", "150", 0); err != nil || page != nil {
		t.Fatalf("page rows were not unsynced: page = %+v, err = %v", page, err)
	}
}

func TestUnsyncRejectsInvalidRange(t *testing.T) {
	path := seedUnsyncState(t)
	if code := Unsync([]string{"alpha-abc123", "200-100"}); code != 2 {
		t.Fatalf("Unsync(invalid range) = %d, want 2", code)
	}
	st := unsyncState(t, path)
	defer st.Close()
	exceptions, err := st.AssumeExceptions("alpha-abc123")
	if err != nil || len(exceptions) != 0 {
		t.Fatalf("invalid range added exceptions: %v, err = %v", exceptions, err)
	}
}
