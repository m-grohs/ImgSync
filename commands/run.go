package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"imgsync/config"
	"imgsync/fetch"
	"imgsync/logging"
	"imgsync/progress"
	"imgsync/state"
	"imgsync/sync"
)

// Run syncs every registered bookmark.
func Run(args []string) int {
	fs := newFlagSet("run", "sync every registered bookmark")
	var series multiFlag
	fs.Var(&series, "series", "only sync this bookmark: `ID|SLUG` (repeatable)")
	dryRun := fs.Bool("dry-run", false, "show what would download")
	verify := fs.Bool("verify", false, "conditional GET for pages the db marks synced")
	limit := fs.Int("limit", -1, "stop after `N` new pages per bookmark (default: no limit)")
	noProgress := fs.Bool("no-progress", false, "hide the progress bar")
	verbose := fs.Bool("verbose", false, "show detailed log output")
	fs.BoolVar(verbose, "v", false, "alias for --verbose")
	yes := fs.Bool("yes", false, "use an existing series folder without asking")
	fs.BoolVar(yes, "y", false, "alias for --yes")
	var demo demoMode
	fs.Var(&demo, "demo", "preview output without syncing: `MODE` (success, empty, error, interrupt)")
	if _, err := parseArgs(fs, args); err != nil {
		return flagExitCode(err)
	}
	if *noProgress {
		progress.SetEnabled(false)
	}
	logging.SetVerbose(*verbose)

	if demo != "" {
		runDemo(os.Stdout, string(demo))
		return 0
	}

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

	start := time.Now()
	result, err := sync.SyncAll(ctx, f, st, sync.Options{
		Slugs:     series,
		DryRun:    *dryRun,
		Verify:    *verify,
		AssumeYes: *yes,
		Limit:     limitPtr,
	})
	elapsed := time.Since(start)

	switch {
	case errors.Is(err, context.Canceled):
		fmt.Fprint(os.Stdout, renderInterrupt(result, elapsed))
		return 130
	case err != nil:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	fmt.Fprint(os.Stdout, renderRunReport(result, elapsed, *dryRun))
	if len(result.Failures()) > 0 {
		return 1
	}
	return 0
}
