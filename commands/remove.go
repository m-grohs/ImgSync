package commands

import (
	"flag"
	"fmt"
	"os"

	"imgsync/config"
	"imgsync/state"
)

// Remove stops syncing a bookmark and keeps the downloaded files.
func Remove(args []string) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(os.Stderr, "usage: imgsync remove <id|slug>")
		return 2
	}
	ref := pos[0]

	st, err := state.Open(config.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer st.Close()

	bookmark, err := st.ResolveBookmark(ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if bookmark == nil {
		fmt.Fprintf(os.Stderr, "not found: %s\n", ref)
		return 1
	}

	removed, err := st.RemoveBookmark(bookmark.Slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if !removed {
		fmt.Fprintf(os.Stderr, "not found: %s\n", ref)
		return 1
	}
	fmt.Printf("removed: %s (downloaded files are kept)\n", bookmark.Slug)
	return 0
}
