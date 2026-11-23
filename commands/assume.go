package commands

import (
	"fmt"
	"os"
	"strconv"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/state"
)

// AssumeSynced marks every chapter up to a cutoff as already downloaded.
func AssumeSynced(args []string) int {
	fs := newFlagSetWithUsage("assume-synced", "trust chapters up to a cutoff as already downloaded", "<id|slug> <chapter> | <id|slug> --clear")
	clear := fs.Bool("clear", false, "remove the assumed cutoff")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExitCode(err)
	}
	if len(pos) == 0 || (*clear && len(pos) != 1) || (!*clear && len(pos) != 2) {
		fmt.Fprintln(os.Stderr, "usage: imgsync assume-synced <id|slug> <chapter> | imgsync assume-synced <id|slug> --clear")
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
		fmt.Fprintf(os.Stderr, "error: no bookmark: %s\n", ref)
		return 1
	}
	slug := bookmark.Slug

	if *clear {
		if err := st.ClearAssumeSynced(slug); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("%s: assumption cleared\n", slug)
		return 0
	}

	cutoff := discover.NormalizeChapter(pos[1])
	f, err := strconv.ParseFloat(cutoff, 64)
	if cutoff == "" || err != nil || f <= 0 {
		fmt.Fprintf(os.Stderr, "error: invalid chapter: %s\n", pos[1])
		return 2
	}

	if err := st.SetAssumeSyncedThrough(slug, cutoff); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("%s: assuming synced through %s\n", slug, discover.ChapterLabel(cutoff))
	return 0
}
