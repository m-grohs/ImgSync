package commands

import (
	"os"
	"strings"
	"testing"

	"imgsync/config"
	"imgsync/state"
)

func seedResetState(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path := config.DBPath()
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertBookmark("old", "", "https://site/comics/old", "old"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertOldBookmarkStillThere(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state db was removed unexpectedly: %v", err)
	}
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.BookmarkID("old"); err != nil {
		t.Fatalf("old bookmark was removed unexpectedly: %v", err)
	}
}

func assertIDsRestartAtOne(t *testing.T, path string) {
	t.Helper()
	st, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.UpsertBookmark("new", "", "https://site/comics/new", "new"); err != nil {
		t.Fatal(err)
	}
	id, err := st.BookmarkID("new")
	if err != nil || id != 1 {
		t.Fatalf("id after reset = %d, err = %v", id, err)
	}
}

func TestResetRequiresConfirmation(t *testing.T) {
	path := seedResetState(t)
	if code := reset(nil, strings.NewReader(""), false); code != 1 {
		t.Fatalf("reset without confirmation = %d, want 1", code)
	}
	assertOldBookmarkStillThere(t, path)

	if code := reset([]string{"--yes"}, strings.NewReader(""), false); code != 0 {
		t.Fatalf("reset --yes = %d, want 0", code)
	}
	assertIDsRestartAtOne(t, path)
}

func TestResetPrompt(t *testing.T) {
	path := seedResetState(t)
	if code := reset(nil, strings.NewReader("n\n"), true); code != 1 {
		t.Fatalf("declined reset = %d, want 1", code)
	}
	assertOldBookmarkStillThere(t, path)

	if code := reset(nil, strings.NewReader("yes\n"), true); code != 0 {
		t.Fatalf("confirmed reset = %d, want 0", code)
	}
	assertIDsRestartAtOne(t, path)
}

func TestResetRejectsPositionalArgs(t *testing.T) {
	path := seedResetState(t)
	if code := reset([]string{"extra", "--yes"}, strings.NewReader(""), false); code != 2 {
		t.Fatalf("reset with positional arg = %d, want 2", code)
	}
	assertOldBookmarkStillThere(t, path)
}
