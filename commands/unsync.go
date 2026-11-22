package commands

import (
	"fmt"
	"os"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/state"
)

// Unsync marks one or all chapters of a bookmark as not synced so the next
// run downloads them again.
func Unsync(args []string) int {
	fs := newFlagSetWithUsage("unsync", "mark chapters as not synced so they download again", "<id|slug> [chapter|range...]")
	dryRun := fs.Bool("dry-run", false, "report only, change nothing")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExitCode(err)
	}
	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "usage: imgsync unsync <id|slug> [chapter|range...] [--dry-run]")
		return 2
	}
	ref := pos[0]
	wanted := pos[1:]

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

	var chapters []string
	assumed := map[string]bool{}
	if len(wanted) > 0 {
		chapters, err = discover.ParseChapterSpecs(wanted)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 2
		}
		if bookmark.AssumeSyncedThrough != "" {
			var exceptions []string
			for _, number := range chapters {
				if discover.ChapterAtOrBefore(number, bookmark.AssumeSyncedThrough) {
					assumed[number] = true
					exceptions = append(exceptions, number)
				}
			}
			if !*dryRun {
				if err := st.AddAssumeExceptions(slug, exceptions); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					return 1
				}
			}
		}
	} else {
		chapters, err = st.AllChaptersForBookmark(slug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if bookmark.AssumeSyncedThrough != "" {
			for _, number := range chapters {
				assumed[number] = true
			}
			if !*dryRun {
				if err := st.ClearAssumeSynced(slug); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					return 1
				}
			}
		}
	}

	if len(chapters) == 0 {
		fmt.Fprintf(os.Stderr, "error: no synced chapters found for %s\n", slug)
		return 1
	}
	discover.SortChapters(chapters)

	total := int64(0)
	var assumedChapters []string
	for _, number := range chapters {
		label := discover.ChapterLabel(number)
		var n int64
		if *dryRun {
			n, err = st.CountPagesForChapters(slug, []string{number})
		} else {
			n, err = st.UnsyncChapters(slug, []string{number})
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		total += n
		if assumed[number] && n == 0 {
			assumedChapters = append(assumedChapters, number)
			continue
		}
		switch {
		case n == 0:
			fmt.Printf("%s: nothing synced\n", label)
		case *dryRun:
			fmt.Printf("%s: would unsync %d page(s)\n", label, n)
		default:
			fmt.Printf("%s: %d page(s) unsynced (will re-download next run)\n", label, n)
		}
	}
	if len(assumedChapters) > 0 {
		verb := "will"
		if *dryRun {
			verb = "would"
		}
		fmt.Printf("%s: assumed synced - %s re-download next run\n",
			discover.ChapterRangeLabel(assumedChapters), verb)
	}

	if *dryRun {
		fmt.Printf("total: %d page(s) would be unsynced [dry run - nothing changed]\n", total)
	} else {
		fmt.Printf("total: %d page(s) unsynced\n", total)
	}
	return 0
}
