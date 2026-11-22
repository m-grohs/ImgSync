package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"imgsync/config"
	"imgsync/state"
	"imgsync/sync"
)

// Export writes bookmarks and their assumed chapters to a file or stdout.
func Export(args []string) int {
	fs := newFlagSet("export", "export bookmarks to a file or stdout")
	formatFlag := fs.String("format", formatAuto, "file format: `txt|csv|json` (default: infer from extension)")
	out := fs.String("o", "", "write to `FILE` instead of stdout")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExitCode(err)
	}
	if len(pos) != 0 {
		fmt.Fprintln(os.Stderr, "usage: imgsync export [-o FILE] [--format txt|csv|json]")
		return 2
	}
	format, err := resolveBookmarksFormat(*formatFlag, *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
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
	entries := make([]bookmarkEntry, 0, len(bookmarks))
	for _, b := range bookmarks {
		exceptionMap, err := st.AssumeExceptions(b.Slug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		exceptions := make([]string, 0, len(exceptionMap))
		for chapter := range exceptionMap {
			exceptions = append(exceptions, chapter)
		}
		entries = append(entries, entryFromBookmark(b, exceptions))
	}
	data, err := encodeBookmarks(entries, format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	dest := "stdout"
	if *out != "" {
		dest = *out
		if _, err := sync.AtomicWrite(filepath.Clean(*out), data); err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot write %s: %v\n", *out, err)
			return 1
		}
	} else if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot write stdout: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "exported %d bookmark(s) to %s (%s)\n", len(entries), dest, format)
	return 0
}
