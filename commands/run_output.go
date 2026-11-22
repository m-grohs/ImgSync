package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"imgsync/discover"
	"imgsync/internal/ui"
	"imgsync/state"
	"imgsync/sync"
)

func renderRunReport(result sync.Result, elapsed time.Duration, dryRun bool) string {
	return renderRunReportStyled(result, elapsed, dryRun, ui.Style{Enabled: ui.ColorsEnabled(os.Stdout)})
}

func renderRunReportStyled(result sync.Result, elapsed time.Duration, dryRun bool, st ui.Style) string {
	var b strings.Builder
	failures := result.Failures()
	stats := summaryStats(result, st)
	if len(failures) > 0 {
		fmt.Fprintf(&b, "%s %s\n\n",
			st.Error(ui.Cross),
			st.Bold(fmt.Sprintf("Sync finished with %d %s in %s",
				len(failures), plural(len(failures), "problem", "problems"), ui.Duration(elapsed))))
		for _, item := range result.Bookmarks {
			if item.Err == nil && item.Summary.Failures() == 0 {
				continue
			}
			fmt.Fprintf(&b, "  %s\n    %s\n", st.Name(seriesName(item.Bookmark)), st.Dim(problemDetail(item)))
		}
		if result.Totals.New > 0 {
			fmt.Fprintf(&b, "\n  %s\n", st.Dim(downloadCount(result.Totals, dryRun)))
		}
		if stats != "" {
			fmt.Fprintf(&b, "  %s\n", stats)
		}
		return b.String()
	}

	switch {
	case result.Totals.New > 0:
		verb := "downloaded"
		if dryRun {
			verb = "would download"
		}
		fmt.Fprintf(&b, "%s %s\n\n",
			st.Success(ui.Check),
			st.Bold(fmt.Sprintf("%s in %s %s %d chapters, %d pages %s",
				successTitle(dryRun), ui.Duration(elapsed), ui.EmDash, len(result.Totals.NewChapters), result.Totals.New, verb)))
		for _, item := range result.Bookmarks {
			if len(item.Summary.NewChapters) == 0 {
				continue
			}
			fmt.Fprintf(&b, "  %s %s %s\n", st.Name(seriesName(item.Bookmark)),
				st.Dim(ui.Dot), discover.ChapterRangeLabel(item.Summary.NewChapters))
		}
	case dryRun:
		fmt.Fprintf(&b, "%s %s\n",
			st.Success(ui.Check), st.Bold(fmt.Sprintf("Dry run complete in %s %s nothing to download",
				ui.Duration(elapsed), ui.EmDash)))
		if stats != "" {
			fmt.Fprintf(&b, "\n  %s\n", stats)
		}
		return b.String()
	default:
		fmt.Fprintf(&b, "%s %s\n",
			st.Success(ui.Check), st.Bold(fmt.Sprintf("All caught up in %s %s no new pages",
				ui.Duration(elapsed), ui.EmDash)))
		if stats != "" {
			fmt.Fprintf(&b, "\n  %s\n", stats)
		}
		return b.String()
	}

	if stats != "" {
		fmt.Fprintf(&b, "\n  %s\n", stats)
	}
	return b.String()
}

func renderInterrupt(result sync.Result, elapsed time.Duration) string {
	st := ui.Style{Enabled: ui.ColorsEnabled(os.Stdout)}
	return renderInterruptStyled(result, elapsed, st)
}

func renderInterruptStyled(result sync.Result, elapsed time.Duration, st ui.Style) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n",
		st.Warning(ui.Warn),
		st.Bold(fmt.Sprintf("Interrupted after %s %s progress saved, rerun to continue",
			ui.Duration(elapsed), ui.EmDash)))
	if result.Totals.New > 0 {
		fmt.Fprintf(&b, "\n  %s\n", st.Dim(downloadCount(result.Totals, false)))
	}
	return b.String()
}

func successTitle(dryRun bool) string {
	if dryRun {
		return "Dry run complete"
	}
	return "Sync complete"
}

func downloadCount(s sync.Summary, dryRun bool) string {
	verb := "pages downloaded"
	if dryRun {
		verb = "pages would download"
	}
	return fmt.Sprintf("%d %s, %d %s", len(s.NewChapters),
		plural(len(s.NewChapters), "chapter", "chapters"), s.New, verb)
}

func summaryStats(result sync.Result, st ui.Style) string {
	parts := make([]string, 0, 6)
	s := result.Totals
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d %s unchanged", s.Skipped, plural(s.Skipped, "page", "pages")))
	}
	if s.Assumed > 0 {
		parts = append(parts, fmt.Sprintf("%d %s assumed", s.Assumed, plural(s.Assumed, "chapter", "chapters")))
	}
	if s.Synced > 0 {
		parts = append(parts, fmt.Sprintf("%d %s up to date", s.Synced, plural(s.Synced, "chapter", "chapters")))
	}
	if s.Locked > 0 {
		parts = append(parts, fmt.Sprintf("%d locked", s.Locked))
	}
	skipped := 0
	for _, item := range result.Bookmarks {
		if item.Skipped && item.Err == nil {
			skipped++
		}
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d %s skipped", skipped, plural(skipped, "bookmark", "bookmarks")))
	}
	parts = append(parts, fmt.Sprintf("%d %s", len(result.Bookmarks), plural(len(result.Bookmarks), "bookmark", "bookmarks")))
	return st.Dim(strings.Join(parts, " "+ui.Dot+" "))
}

func seriesName(b state.Bookmark) string {
	if b.Title != "" {
		return b.Title
	}
	return b.Slug
}

func problemDetail(item sync.BookmarkResult) string {
	if item.Err != nil {
		return item.Err.Error()
	}
	s := item.Summary
	switch {
	case s.Failed > 0 && s.FailedChapters > 0:
		return fmt.Sprintf("%d %s failed, %d %s failed",
			s.Failed, plural(s.Failed, "page", "pages"), s.FailedChapters, plural(s.FailedChapters, "chapter", "chapters"))
	case s.Failed > 0:
		return fmt.Sprintf("%d %s failed", s.Failed, plural(s.Failed, "page", "pages"))
	case s.FailedChapters > 0:
		return fmt.Sprintf("%d %s failed", s.FailedChapters, plural(s.FailedChapters, "chapter", "chapters"))
	default:
		return "incomplete"
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
