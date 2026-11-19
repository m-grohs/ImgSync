package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"imgsync/config"
	"imgsync/fetch"
	"imgsync/state"
	"imgsync/sync"
)

// Run syncs every registered bookmark.
func Run(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var series multiFlag
	fs.Var(&series, "series", "only sync this bookmark slug (repeatable)")
	dryRun := fs.Bool("dry-run", false, "show what would download")
	verify := fs.Bool("verify", false, "conditional GET for pages the db marks synced")
	limit := fs.Int("limit", -1, "stop after N new pages per bookmark")
	if _, err := parseArgs(fs, args); err != nil {
		return 2
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

	f := fetch.New()
	defer f.Close()

	totals, failures, err := sync.SyncAll(f, st, series, *dryRun, *verify, limitPtr)
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
