package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"imgsync/config"
	"imgsync/fetch"
	"imgsync/state"
	"imgsync/sync"
)

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// Add registers one or more series URLs as bookmarks.
func Add(args []string) int {
	fs := newFlagSet("add", "register one or more series URLs as bookmarks")
	var files multiFlag
	fs.Var(&files, "f", "read URLs from `FILE` (one per line)")
	fs.Var(&files, "file", "read URLs from `FILE` (one per line)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExitCode(err)
	}

	urls := append([]string{}, pos...)
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot read %s: %v\n", p, err)
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			urls = append(urls, line)
		}
	}

	if len(urls) == 0 {
		fmt.Fprintln(os.Stderr, "error: no URLs given (pass one or more, or -f FILE)")
		return 2
	}

	st, err := state.Open(config.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	f := fetch.New()
	defer f.Close()

	rc := 0
	for _, u := range urls {
		slug, title, created, err := sync.AddSeries(ctx, f, st, u)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed: %s: %v\n", u, err)
			rc = 1
			continue
		}
		verb := "added"
		if !created {
			verb = "updated"
			if b, err := st.GetBookmark(slug); err == nil && b != nil {
				if b.Location != "" || b.AssumeSyncedThrough != "" {
					slog.Warn("existing bookmark keeps its location and assumption",
						"bookmark", slug, "location", b.Location, "assumed_through", b.AssumeSyncedThrough)
				}
			}
		}
		id, idErr := st.BookmarkID(slug)
		if idErr != nil {
			fmt.Println(strings.TrimRight(fmt.Sprintf("%s: %s  %s", verb, slug, title), " "))
			continue
		}
		fmt.Println(strings.TrimRight(fmt.Sprintf("%s: #%d  %s  %s", verb, id, slug, title), " "))
	}
	return rc
}
