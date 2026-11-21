package state

import (
	"path/filepath"
	"testing"
)

func TestApplyImport(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.UpsertBookmark("alpha-abc123", "abc123", "https://site.example/comics/alpha-abc123", "Old"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssumeSyncedThrough("alpha-abc123", "050"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAssumeException("alpha-abc123", "010"); err != nil {
		t.Fatal(err)
	}

	empty := []string{}
	enabled := false
	results, err := st.ApplyImport([]ImportBookmark{
		{
			Slug:     "alpha-abc123",
			URL:      "https://site.example/comics/alpha-abc123",
			SeriesID: "abc123",
			Assume:   ptrString("none"),
			Except:   &empty,
			Enabled:  &enabled,
		},
		{
			Slug:     "beta-def456",
			URL:      "https://site.example/comics/beta-def456",
			SeriesID: "def456",
			Assume:   ptrString("007"),
			Title:    ptrString("Beta"),
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !results[0].Updated || !results[1].Created {
		t.Fatalf("results = %+v", results)
	}

	alpha, err := st.GetBookmark("alpha-abc123")
	if err != nil || alpha == nil {
		t.Fatalf("alpha = %+v, err = %v", alpha, err)
	}
	if alpha.AssumeSyncedThrough != "" || alpha.Enabled {
		t.Fatalf("alpha = %+v, want cleared assumption and disabled", alpha)
	}
	alphaExceptions, err := st.AssumeExceptions("alpha-abc123")
	if err != nil || len(alphaExceptions) != 0 {
		t.Fatalf("alpha exceptions = %v, err = %v", alphaExceptions, err)
	}
	beta, err := st.GetBookmark("beta-def456")
	if err != nil || beta == nil {
		t.Fatalf("beta = %+v, err = %v", beta, err)
	}
	if beta.AssumeSyncedThrough != "007" || beta.Title != "Beta" || !beta.Enabled {
		t.Fatalf("beta = %+v", beta)
	}
	if id, err := st.BookmarkID("beta-def456"); err != nil || id == 0 {
		t.Fatalf("beta id = %d, err = %v", id, err)
	}

	results, err = st.ApplyImport([]ImportBookmark{{
		Slug:   "alpha-abc123",
		URL:    "https://site.example/comics/alpha-abc123",
		Assume: ptrString("099"),
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Kept {
		t.Fatalf("results without update = %+v", results)
	}
	alpha, err = st.GetBookmark("alpha-abc123")
	if err != nil || alpha == nil || alpha.AssumeSyncedThrough != "" {
		t.Fatalf("alpha changed without update: %+v, err = %v", alpha, err)
	}
}

func ptrString(s string) *string { return &s }
