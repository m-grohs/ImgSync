package commands

import (
	"strings"
	"testing"
	"time"

	"imgsync/internal/ui"
	"imgsync/state"
	"imgsync/sync"
)

func TestStyledOutputUsesBoldInsteadOfTeal(t *testing.T) {
	st := ui.Style{Enabled: true}
	bookmark := state.Bookmark{ID: 1, Slug: "series-a", Title: "Series A", AssumeSyncedThrough: "127"}

	list := renderListSummary([]state.Bookmark{bookmark}, st) +
		renderListDetail(bookmark, nil, st)
	result := sync.Result{Totals: sync.Summary{New: 18, NewChapters: []string{"338"}}}
	result.Bookmarks = []sync.BookmarkResult{{
		Bookmark: bookmark,
		Summary:  result.Totals,
	}}
	report := renderRunReportStyled(result, time.Second, false, st) +
		renderInterruptStyled(result, time.Second, st)

	for name, out := range map[string]string{"list": list, "report": report} {
		if strings.Contains(out, ";36m") {
			t.Errorf("%s output still uses teal:\n%s", name, out)
		}
		if !strings.Contains(out, "\x1b[1mSeries A\x1b[0m") {
			t.Errorf("%s output missing bold series name:\n%s", name, out)
		}
	}
	if !strings.Contains(report, "\x1b[1;32m✓\x1b[0m") {
		t.Errorf("report output missing green check:\n%s", report)
	}
}
