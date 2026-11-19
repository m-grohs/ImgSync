package commands

import (
	"flag"
	"fmt"
	"os"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/state"
)

// Unsync marks one or all chapters of a bookmark as not synced so the next
// run downloads them again.
func Unsync(args []string) int {
	fs := flag.NewFlagSet("unsync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dryRun := fs.Bool("dry-run", false, "report only, change nothing")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "usage: imgsync unsync <id|slug> [chapter...] [--dry-run]")
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
		seen := map[string]bool{}
		for _, arg := range wanted {
			number := discover.NormalizeChapter(arg)
			if number == "" {
				fmt.Fprintf(os.Stderr, "warning: cannot parse chapter %q for %s\n", arg, slug)
				continue
			}
			if !seen[number] {
				seen[number] = true
				chapters = append(chapters, number)
			}
		}
		if bookmark.AssumeSyncedThrough != "" {
			for _, number := range chapters {
				if !discover.ChapterAtOrBefore(number, bookmark.AssumeSyncedThrough) {
					continue
				}
				assumed[number] = true
				if *dryRun {
					continue
				}
				if err := st.AddAssumeException(slug, number); err != nil {
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
		switch {
		case n == 0 && assumed[number]:
			if *dryRun {
				fmt.Printf("%s: assumed synced - would re-download next run\n", label)
			} else {
				fmt.Printf("%s: assumed synced - will re-download next run\n", label)
			}
		case n == 0:
			fmt.Printf("%s: nothing synced\n", label)
		case *dryRun:
			fmt.Printf("%s: would unsync %d page(s)\n", label, n)
		default:
			fmt.Printf("%s: %d page(s) unsynced (will re-download next run)\n", label, n)
		}
	}

	if *dryRun {
		fmt.Printf("total: %d page(s) would be unsynced [dry run - nothing changed]\n", total)
	} else {
		fmt.Printf("total: %d page(s) unsynced\n", total)
	}
	return 0
}
