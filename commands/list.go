package commands

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"imgsync/config"
	"imgsync/state"
)

// List prints the registered bookmarks.
func List(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if _, err := parseArgs(fs, args); err != nil {
		return 2
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
	fmt.Fprintln(w, "SLUG\tCH\tPAGES\tLOCATION\tLAST RUN\tTITLE")
	for _, b := range bookmarks {
		location := "-"
		if b.Location != "" {
			location = b.Location
		}
		lastRun := "-"
		if b.LastRun != "" {
			lastRun = b.LastRun
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%s\n",
			b.Slug, b.Chapters, b.Pages, location, lastRun, b.Title)
	}
	_ = w.Flush()
	return 0
}
