package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/fetch"
	"imgsync/state"
)

// Import adds bookmarks from a txt, csv or json file.
func Import(args []string) int {
	fs := newFlagSetWithUsage("import", "import bookmarks from a file (- reads stdin)", "FILE")
	formatFlag := fs.String("format", formatAuto, "file format: `txt|csv|json` (default: infer from extension)")
	dryRun := fs.Bool("dry-run", false, "report changes without writing them")
	update := fs.Bool("update", false, "apply file fields to bookmarks that already exist")
	online := fs.Bool("online", false, "fetch titles for new bookmarks from the site")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExitCode(err)
	}
	if len(pos) != 1 {
		fmt.Fprintln(os.Stderr, "usage: imgsync import FILE [--format txt|csv|json] [--dry-run] [--update] [--online]")
		return 2
	}

	path := pos[0]
	var data []byte
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot read %s: %v\n", path, err)
		return 1
	}
	format, err := resolveBookmarksFormat(*formatFlag, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	entries, warnings, err := decodeBookmarks(data, format)
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid %s file %s: %v\n", format, path, err)
		return 2
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "error: no bookmarks found in file")
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
	known := make(map[string]bool, len(bookmarks))
	for _, b := range bookmarks {
		known[b.Slug] = true
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var f *fetch.Fetcher
	if *online {
		f = fetch.New()
		defer f.Close()
	}

	imports := make([]state.ImportBookmark, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		if *online && !known[e.Slug] && e.Title == nil {
			if doc, fetchErr := f.GetText(ctx, e.Canonical); fetchErr != nil {
				fmt.Fprintf(os.Stderr, "warning: %s: cannot fetch title: %v\n", e.source, fetchErr)
			} else if title := discover.SeriesTitle(doc); title != "" {
				value := title
				e.Title = &value
			}
		}
		if e.Location != nil && *e.Location != "none" {
			expanded := config.ExpandPath(*e.Location)
			info, statErr := os.Stat(expanded)
			if statErr != nil || !info.IsDir() {
				fmt.Fprintf(os.Stderr, "warning: %s: location not found, ignoring: %s\n", e.source, *e.Location)
				e.Location = nil
			} else {
				e.Location = &expanded
			}
		}
		imports = append(imports, e.toStateImport())
	}

	if *dryRun {
		seen := make(map[string]bool, len(known))
		for slug := range known {
			seen[slug] = true
		}
		var added, updated, kept int
		for _, e := range imports {
			switch {
			case !seen[e.Slug]:
				added++
				fmt.Printf("would add: %s\n", e.Slug)
			case *update:
				updated++
				fmt.Printf("would update: %s\n", e.Slug)
			default:
				kept++
				fmt.Printf("would keep: %s (pass --update to apply file fields)\n", e.Slug)
			}
			seen[e.Slug] = true
		}
		fmt.Printf("total: %d added, %d updated, %d kept [dry run - nothing changed]\n", added, updated, kept)
		return 0
	}

	results, err := st.ApplyImport(imports, *update)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: import failed: %v\n", err)
		return 1
	}
	var added, updated, kept int
	for _, result := range results {
		switch {
		case result.Created:
			added++
			fmt.Printf("added: %s\n", result.Slug)
		case result.Updated:
			updated++
			fmt.Printf("updated: %s\n", result.Slug)
		case result.Kept:
			kept++
			fmt.Printf("kept: %s (pass --update to apply file fields)\n", result.Slug)
		}
	}
	fmt.Printf("total: %d added, %d updated, %d kept\n", added, updated, kept)
	return 0
}
