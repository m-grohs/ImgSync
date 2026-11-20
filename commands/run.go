package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"imgsync/config"
	"imgsync/fetch"
	"imgsync/logging"
	"imgsync/progress"
	"imgsync/state"
	"imgsync/sync"
)

// Run syncs every registered bookmark.
func Run(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var series multiFlag
	fs.Var(&series, "series", "only sync this bookmark: id, unique slug prefix or slug (repeatable)")
	dryRun := fs.Bool("dry-run", false, "show what would download")
	verify := fs.Bool("verify", false, "conditional GET for pages the db marks synced")
	limit := fs.Int("limit", -1, "stop after N new pages per bookmark")
	noProgress := fs.Bool("no-progress", false, "hide the progress bar")
	verbose := fs.Bool("verbose", false, "show per-chapter details while indexing")
	fs.BoolVar(verbose, "v", false, "alias for --verbose")
	yes := fs.Bool("yes", false, "use an existing series folder without asking")
	fs.BoolVar(yes, "y", false, "alias for --yes")
	if _, err := parseArgs(fs, args); err != nil {
		return 2
	}
	if *noProgress {
		progress.SetEnabled(false)
	}
	logging.SetVerbose(*verbose)

	var limitPtr *int
	if *limit >= 0 {
		limitPtr = limit
	}

	st, err := state.Open(config.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer st.Close()

	for i, ref := range series {
		bookmark, err := st.ResolveBookmark(ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if bookmark == nil {
			fmt.Fprintf(os.Stderr, "error: unknown bookmark: %s\n", ref)
			return 1
		}
		series[i] = bookmark.Slug
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	f := fetch.New()
	defer f.Close()

	totals, failures, err := sync.SyncAll(ctx, f, st, sync.Options{
		Slugs:     series,
		DryRun:    *dryRun,
		Verify:    *verify,
		AssumeYes: *yes,
		Limit:     limitPtr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	fmt.Printf("done: %s\n", totals.Text())
	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "bookmarks with issues: %s\n", strings.Join(failures, ", "))
		return 1
	}
	return 0
}
