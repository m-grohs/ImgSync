package commands

import (
	"fmt"
	"os"
	"text/tabwriter"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/state"
)

// List prints the registered bookmarks.
func List(args []string) int {
	fs := newFlagSet("list", "show registered bookmarks")
	if _, err := parseArgs(fs, args); err != nil {
		return flagExitCode(err)
	}

	st, err := state.Open(config.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer st.Close()

	bookmarks, err := st.ListBookmarks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(bookmarks) == 0 {
		fmt.Println("no bookmarks yet - add one: imgsync add <url>")
		return 0
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSLUG\tCH\tPAGES\tASSUMED\tLOCATION\tLAST RUN\tTITLE")
	for _, b := range bookmarks {
		location := "-"
		if b.Location != "" {
			location = b.Location
		}
		lastRun := "-"
		if b.LastRun != "" {
			lastRun = b.LastRun
		}
		assumed := "-"
		if b.AssumeSyncedThrough != "" {
			assumed = discover.ChapterLabel(b.AssumeSyncedThrough)
		}
		fmt.Fprintf(w, "%d\t%s\t%d\t%d\t%s\t%s\t%s\t%s\n",
			b.ID, b.Slug, b.Chapters, b.Pages, assumed, location, lastRun, b.Title)
	}
	_ = w.Flush()
	return 0
}
