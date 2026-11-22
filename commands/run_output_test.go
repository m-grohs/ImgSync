package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"imgsync/internal/ui"
	"imgsync/state"
	"imgsync/sync"
)

func TestRenderRunReportSuccess(t *testing.T) {
	result := sync.Result{}
	items := []sync.BookmarkResult{
		{
			Bookmark: state.Bookmark{Title: "Series A", Slug: "a"},
			Summary:  sync.Summary{New: 71, NewChapters: []string{"338", "339", "343", "344"}, Skipped: 291, Assumed: 807},
		},
		{
			Bookmark: state.Bookmark{Title: "Series B", Slug: "b"},
			Summary:  sync.Summary{Assumed: 486},
		},
	}
	for _, item := range items {
		result.Totals.Add(item.Summary)
		result.Bookmarks = append(result.Bookmarks, item)
	}

	got := renderRunReportStyled(result, 64*time.Second, false, ui.Style{})
	for _, want := range []string{
		"✓ Sync complete in 1m 04s — 4 chapters, 71 pages downloaded",
		"Series A · Chapter 338-339, 343-344",
		"291 pages unchanged · 1293 chapters assumed · 2 bookmarks",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report =\n%s\nwant %q", got, want)
		}
	}
}

func TestRenderRunReportAllCaughtUp(t *testing.T) {
	result := sync.Result{Totals: sync.Summary{Skipped: 4}, Bookmarks: []sync.BookmarkResult{{
		Bookmark: state.Bookmark{Slug: "a"},
		Summary:  sync.Summary{Skipped: 4},
	}}}
	got := renderRunReportStyled(result, 42*time.Second, false, ui.Style{})
	if !strings.Contains(got, "✓ All caught up in 42s — no new pages") {
		t.Errorf("report = %q", got)
	}
	if strings.Contains(got, "downloaded") {
		t.Errorf("report should not mention downloads: %q", got)
	}
}

func TestRenderRunReportErrors(t *testing.T) {
	result := sync.Result{Bookmarks: []sync.BookmarkResult{
		{
			Bookmark: state.Bookmark{Title: "Series A", Slug: "a"},
			Summary:  sync.Summary{New: 13, NewChapters: []string{"074"}},
		},
		{
			Bookmark: state.Bookmark{Title: "Series B", Slug: "b"},
			Summary:  sync.Summary{Failed: 3},
		},
		{
			Bookmark: state.Bookmark{Title: "Series C", Slug: "c"},
			Err:      errors.New("HTTP 403"),
		},
	}}
	result.Totals.Add(result.Bookmarks[0].Summary)
	result.Totals.Add(result.Bookmarks[1].Summary)
	result.Totals.FailedBookmarks = 1

	got := renderRunReportStyled(result, 5*time.Second, false, ui.Style{})
	for _, want := range []string{
		"✗ Sync finished with 2 problems in 5s",
		"Series B\n    3 pages failed",
		"Series C\n    HTTP 403",
		"1 chapter, 13 pages downloaded",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report =\n%s\nwant %q", got, want)
		}
	}
}

func TestRenderInterrupt(t *testing.T) {
	summary := sync.Summary{New: 18, NewChapters: []string{"338"}}
	result := sync.Result{Totals: summary, Bookmarks: []sync.BookmarkResult{{
		Bookmark: state.Bookmark{Title: "Series A", Slug: "a"},
		Summary:  summary,
		Err:      context.Canceled,
	}}}
	got := renderInterruptStyled(result, 42*time.Second, ui.Style{})
	for _, want := range []string{
		"⚠ Interrupted after 42s — progress saved, rerun to continue",
		"1 chapter, 18 pages downloaded",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report =\n%s\nwant %q", got, want)
		}
	}
}
