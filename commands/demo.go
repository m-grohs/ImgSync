package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"imgsync/internal/ui"
	"imgsync/progress"
	"imgsync/state"
	"imgsync/sync"
)

// demoMode is an optional-value flag: --demo selects the success scenario and
// --demo=MODE selects another one.
type demoMode string

func (d *demoMode) String() string { return string(*d) }

func (d *demoMode) Set(value string) error {
	switch strings.ToLower(value) {
	case "", "1", "true", "yes", "success":
		*d = "success"
	case "empty", "error", "interrupt":
		*d = demoMode(strings.ToLower(value))
	default:
		return fmt.Errorf("unknown demo mode %q (success, empty, error, interrupt)", value)
	}
	return nil
}

func (d *demoMode) IsBoolFlag() bool { return true }

// runDemo exercises the real progress renderer and summary renderer with
// sample data, without touching the database, the network or downloaded files.
func runDemo(out io.Writer, mode string) {
	start := time.Now()
	st := ui.Style{Enabled: ui.ColorsEnabled(out)}

	switch mode {
	case "empty":
		fmt.Fprint(out, renderRunReportStyled(demoEmptyResult(), time.Since(start), false, st))
	case "error":
		demoBars(false)
		fmt.Fprint(out, renderRunReportStyled(demoErrorResult(), time.Since(start), false, st))
	case "interrupt":
		demoBars(true)
		fmt.Fprint(out, renderInterruptStyled(demoInterruptResult(), time.Since(start), st))
	default:
		demoBars(false)
		fmt.Fprint(out, renderRunReportStyled(demoSuccessResult(), time.Since(start), false, st))
	}
}

func demoBars(partial bool) {
	series := progress.New("Indexing", "", "series", 3)
	for i := 0; i < 3; i++ {
		demoPause()
		series.Add(1)
	}
	series.Clear()

	index := progress.New("Indexing", "", "chapters", 12)
	for i, name := range []string{"The Beginning After the End", "The Divine Demon's Grand Ascension", "Red Storm"} {
		index.SetDetail(name)
		for j := 0; j < 4; j++ {
			demoPause()
			index.Add(1)
		}
		if partial && i == 0 {
			break
		}
	}
	index.Clear()

	if partial {
		bar := progress.New("The Beginning After the End", "Chapter 338", "pages", 24)
		for i := 0; i < 8; i++ {
			demoPause()
			bar.Add(1)
		}
		bar.Clear()
		return
	}

	for _, series := range []struct {
		name     string
		chapters []string
		pages    int
	}{
		{"The Beginning After the End", []string{"Chapter 338", "Chapter 339", "Chapter 343", "Chapter 344"}, 58},
		{"The Divine Demon's Grand Ascension", []string{"Chapter 074"}, 13},
	} {
		bar := progress.New(series.name, series.chapters[0], "pages", series.pages)
		step := series.pages / len(series.chapters)
		for _, chapter := range series.chapters {
			bar.SetDetail(chapter)
			for i := 0; i < step; i++ {
				demoPause()
				bar.Add(1)
			}
		}
		bar.Clear()
	}
}

func demoPause() {
	if progress.Enabled() {
		time.Sleep(35 * time.Millisecond)
	}
}

func demoBookmark(title, slug string) state.Bookmark {
	return state.Bookmark{Title: title, Slug: slug}
}

func demoSuccessResult() sync.Result {
	result := sync.Result{}
	items := []sync.BookmarkResult{
		{
			Bookmark: demoBookmark("The Beginning After the End", "the-beginning-after-the-end"),
			Summary: sync.Summary{
				New: 58, NewChapters: []string{"338", "339", "343", "344"}, Skipped: 291, Assumed: 248,
			},
		},
		{
			Bookmark: demoBookmark("The Divine Demon's Grand Ascension", "the-divine-demons-grand-ascension"),
			Summary:  sync.Summary{New: 13, NewChapters: []string{"074"}, Assumed: 73},
		},
		{
			Bookmark: demoBookmark("Red Storm", "red-storm"),
			Summary:  sync.Summary{Assumed: 486},
		},
	}
	for _, item := range items {
		result.Totals.Add(item.Summary)
		result.Bookmarks = append(result.Bookmarks, item)
	}
	return result
}

func demoEmptyResult() sync.Result {
	result := sync.Result{}
	items := []sync.BookmarkResult{
		{
			Bookmark: demoBookmark("Red Storm", "red-storm"),
			Summary:  sync.Summary{Skipped: 291, Assumed: 486, Synced: 12},
		},
		{
			Bookmark: demoBookmark("Solo Leveling Ragnarok", "solo-leveling-ragnarok"),
			Summary:  sync.Summary{Assumed: 73, Locked: 1},
		},
	}
	for _, item := range items {
		result.Totals.Add(item.Summary)
		result.Bookmarks = append(result.Bookmarks, item)
	}
	return result
}

func demoErrorResult() sync.Result {
	result := sync.Result{}
	ok := sync.BookmarkResult{
		Bookmark: demoBookmark("The Divine Demon's Grand Ascension", "the-divine-demons-grand-ascension"),
		Summary:  sync.Summary{New: 13, NewChapters: []string{"074"}, Assumed: 73},
	}
	result.Totals.Add(ok.Summary)
	result.Bookmarks = append(result.Bookmarks,
		ok,
		sync.BookmarkResult{
			Bookmark: demoBookmark("Red Storm", "red-storm"),
			Summary:  sync.Summary{Failed: 3, Assumed: 486},
		},
		sync.BookmarkResult{
			Bookmark: demoBookmark("The Beginning After the End", "the-beginning-after-the-end"),
			Err:      errors.New("HTTP 403 (blocked - the site may require different headers or is challenging automated clients)"),
		},
	)
	result.Totals.FailedBookmarks = 1
	return result
}

func demoInterruptResult() sync.Result {
	summary := sync.Summary{New: 18, NewChapters: []string{"338"}}
	return sync.Result{
		Totals: summary,
		Bookmarks: []sync.BookmarkResult{{
			Bookmark: demoBookmark("The Beginning After the End", "the-beginning-after-the-end"),
			Summary:  summary,
		}},
	}
}
