package commands

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"imgsync/discover"
	"imgsync/internal/ui"
	"imgsync/state"
)

type listRow struct {
	id      string
	name    string
	assumed string
}

func renderListSummary(bookmarks []state.Bookmark, st ui.Style) string {
	var b strings.Builder
	if len(bookmarks) == 0 {
		fmt.Fprintf(&b, "%s %s\n\n", st.Success(ui.Check), st.Bold("No bookmarks yet"))
		fmt.Fprintf(&b, "  %s\n", st.Dim("add one with 'imgsync add <url>'"))
		return b.String()
	}

	fmt.Fprintf(&b, "%s %s %s\n\n", st.Name("imgsync list"), ui.EmDash, st.Bold("registered bookmarks"))
	sorted := append([]state.Bookmark(nil), bookmarks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	rows := make([]listRow, 0, len(sorted))
	idWidth, nameWidth := 0, 0
	for _, bookmark := range sorted {
		row := listRow{
			id:      fmt.Sprintf("#%d", bookmark.ID),
			name:    seriesName(bookmark),
			assumed: assumedLabel(bookmark, st),
		}
		if n := utf8.RuneCountInString(row.id); n > idWidth {
			idWidth = n
		}
		if n := utf8.RuneCountInString(row.name); n > nameWidth {
			nameWidth = n
		}
		rows = append(rows, row)
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "  %s%s  %s%s  %s\n",
			st.Name(row.id), strings.Repeat(" ", idWidth-utf8.RuneCountInString(row.id)),
			st.Name(row.name), strings.Repeat(" ", nameWidth-utf8.RuneCountInString(row.name)),
			row.assumed)
	}
	fmt.Fprintf(&b, "\n  %s\n", st.Dim(fmt.Sprintf("%d %s · 'imgsync list <id>' for details",
		len(bookmarks), plural(len(bookmarks), "bookmark", "bookmarks"))))
	return b.String()
}

func renderListDetail(bookmark state.Bookmark, exceptions map[string]bool, st ui.Style) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s\n\n",
		st.Name(fmt.Sprintf("Bookmark #%d", bookmark.ID)), ui.EmDash, st.Bold(seriesName(bookmark)))

	rows := []struct {
		label string
		value string
	}{
		{"Slug", st.Name(bookmark.Slug)},
		{"URL", bookmark.URL},
		{"Series ID", valueOrDash(bookmark.SeriesID)},
		{"Status", enabledValue(bookmark.Enabled, st)},
		{"Assumed", assumedChapter(bookmark, st)},
		{"Exceptions", exceptionLabel(exceptions, st)},
		{"Chapters", strconv.Itoa(bookmark.Chapters)},
		{"Pages", strconv.Itoa(bookmark.Pages)},
		{"Location", valueOrDash(bookmark.Location)},
		{"Added", valueOrDash(bookmark.AddedAt)},
		{"Last run", valueOrDash(bookmark.LastRun)},
		{"Last result", resultValue(bookmark.LastResult, st)},
	}
	width := 0
	for _, row := range rows {
		if n := utf8.RuneCountInString(row.label); n > width {
			width = n
		}
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "  %s%s  %s\n",
			st.Dim(row.label), strings.Repeat(" ", width-utf8.RuneCountInString(row.label)), row.value)
	}
	return b.String()
}

func assumedLabel(bookmark state.Bookmark, st ui.Style) string {
	if bookmark.AssumeSyncedThrough == "" {
		return st.Dim(ui.EmDash)
	}
	return st.Dim("assumed through ") + assumedChapter(bookmark, st)
}

func assumedChapter(bookmark state.Bookmark, st ui.Style) string {
	if bookmark.AssumeSyncedThrough == "" {
		return st.Dim(ui.EmDash)
	}
	return st.Name(discover.ChapterLabel(bookmark.AssumeSyncedThrough))
}

func exceptionLabel(exceptions map[string]bool, st ui.Style) string {
	if len(exceptions) == 0 {
		return st.Dim(ui.EmDash)
	}
	chapters := make([]string, 0, len(exceptions))
	for chapter := range exceptions {
		chapters = append(chapters, chapter)
	}
	discover.SortChapters(chapters)
	return st.Name(discover.ChapterRangeLabel(chapters))
}

func enabledValue(enabled bool, st ui.Style) string {
	if enabled {
		return st.Success(ui.Check + " enabled")
	}
	return st.Warning(ui.Warn + " disabled")
}

func resultValue(result string, st ui.Style) string {
	switch result {
	case "ok":
		return st.Success(ui.Check + " ok")
	case "error", "partial":
		return st.Error(ui.Cross + " " + result)
	case "skipped", "interrupted":
		return st.Warning(ui.Warn + " " + result)
	default:
		return st.Dim(ui.EmDash)
	}
}

func valueOrDash(value string) string {
	if value == "" {
		return ui.EmDash
	}
	return value
}
