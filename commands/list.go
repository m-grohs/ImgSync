package commands

import (
	"fmt"
	"io"
	"os"

	"imgsync/config"
	"imgsync/internal/ui"
	"imgsync/state"
)

const listSynopsis = "show registered bookmarks, or details for one bookmark"

// List prints the registered bookmarks, or one bookmark's full details.
func List(args []string) int {
	return list(args, os.Stdout)
}

func list(args []string, out io.Writer) int {
	fs := newFlagSetWithUsage("list", listSynopsis, "[<id|slug>]")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExitCode(err)
	}
	if len(pos) > 1 {
		fmt.Fprintln(os.Stderr, "error: specify at most one bookmark")
		fs.Usage()
		return 2
	}

	store, err := state.Open(config.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer store.Close()

	style := ui.Style{Enabled: ui.ColorsEnabled(out)}
	if len(pos) == 0 {
		bookmarks, err := store.ListBookmarks()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprint(out, renderListSummary(bookmarks, style))
		return 0
	}

	bookmark, err := store.ResolveBookmark(pos[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if bookmark == nil {
		fmt.Fprintf(os.Stderr, "error: unknown bookmark: %s\n", pos[0])
		return 1
	}
	exceptions, err := store.AssumeExceptions(bookmark.Slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprint(out, renderListDetail(*bookmark, exceptions, style))
	return 0
}
