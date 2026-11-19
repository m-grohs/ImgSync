package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/state"
)

type diskPage struct {
	index int
	path  string
}

// SetLocation points a bookmark at a filesystem folder, integrates the
// chapters/pages found there into the database and reports what is missing.
func SetLocation(args []string) int {
	fs := flag.NewFlagSet("set-location", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dryRun := fs.Bool("dry-run", false, "report only, change nothing")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 2 {
		fmt.Fprintln(os.Stderr, "usage: imgsync set-location <slug> <path> [--dry-run]")
		return 2
	}
	slug := pos[0]
	location := config.ExpandPath(pos[1])

	st, err := state.Open(config.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer st.Close()

	bookmark, err := st.GetBookmark(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if bookmark == nil {
		fmt.Fprintf(os.Stderr, "error: no bookmark: %s\n", slug)
		return 1
	}

	if !*dryRun {
		if err := os.MkdirAll(location, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot create %s: %v\n", location, err)
			return 1
		}
	}

	scanned, err := scanLocation(location)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot scan %s: %v\n", location, err)
		return 1
	}

	chapters := make([]string, 0, len(scanned))
	for number := range scanned {
		chapters = append(chapters, number)
	}
	discover.SortChapters(chapters)

	pagesOnDisk := 0
	alreadySynced := 0
	integrated := 0
	relinked := 0

	for _, number := range chapters {
		pages := scanned[number]
		pagesOnDisk += len(pages)

		if !*dryRun {
			if err := st.EnsureChapter(slug, number, len(pages)); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return 1
			}
		}

		for _, page := range pages {
			existing, err := st.GetPage(slug, number, page.index)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return 1
			}
			if existing != nil {
				alreadySynced++
				if existing.Path != page.path {
					if *dryRun {
						relinked++
						continue
					}
					if err := st.SetPagePath(slug, number, page.index, page.path); err != nil {
						fmt.Fprintf(os.Stderr, "error: %v\n", err)
						return 1
					}
					relinked++
				}
				continue
			}
			if *dryRun {
				integrated++
				continue
			}
			digest, size, err := hashFile(page.path)
			if err != nil {
				slog.Warn("cannot hash page, skipping integration", "path", page.path, "error", err)
				continue
			}
			inserted, err := st.IntegratePage(slug, number, page.index, page.path, digest, size)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return 1
			}
			if inserted {
				integrated++
			}
		}
	}

	missing := []state.Page{}
	allPages, err := st.PagesForBookmark(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	for _, p := range allPages {
		if _, onDisk := diskPageAt(scanned, p.Chapter, p.Index); onDisk {
			continue
		}
		if p.Path == "" {
			missing = append(missing, p)
			continue
		}
		if _, err := os.Stat(p.Path); err != nil {
			missing = append(missing, p)
		}
	}

	if !*dryRun {
		if err := st.SetLocation(slug, location); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	title := bookmark.Title
	if title == "" {
		title = "-"
	}
	fmt.Printf("bookmark: %s  %s\n", slug, title)
	fmt.Printf("location: %s\n", location)
	fmt.Printf("chapters found on disk: %d\n", len(chapters))
	fmt.Printf("pages on disk:          %d\n", pagesOnDisk)
	fmt.Printf("already in db:          %d\n", alreadySynced)
	if *dryRun {
		fmt.Printf("would integrate:        %d\n", integrated)
		fmt.Printf("would relink paths:     %d\n", relinked)
	} else {
		fmt.Printf("integrated:             %d\n", integrated)
		fmt.Printf("relinked paths:         %d\n", relinked)
	}
	fmt.Printf("missing (db says synced, not on disk): %d\n", len(missing))
	for i, p := range missing {
		if i >= 20 {
			fmt.Printf("  ... and %d more\n", len(missing)-20)
			break
		}
		fmt.Printf("  - %s page %d: %s\n", discover.ChapterLabel(p.Chapter), p.Index+1, p.Path)
	}
	if *dryRun {
		fmt.Println("[dry run - nothing changed]")
	}
	return 0
}

// diskPageAt returns the on-disk path of a page found during the scan.
func diskPageAt(scanned map[string][]diskPage, chapter string, index int) (string, bool) {
	for _, p := range scanned[chapter] {
		if p.index == index {
			return p.path, true
		}
	}
	return "", false
}

// scanLocation finds chapter folders ("Chapter 001", case-insensitive) and page files
// ("001.webp") under root.
func scanLocation(root string) (map[string][]diskPage, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := map[string][]diskPage{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		number, ok := discover.ParseChapterFolderName(entry.Name())
		if !ok {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			slog.Warn("cannot read chapter folder", "path", dir, "error", err)
			continue
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			index, ok := discover.ParsePageFileName(file.Name())
			if !ok {
				continue
			}
			out[number] = append(out[number], diskPage{
				index: index,
				path:  filepath.Join(dir, file.Name()),
			})
		}
	}
	for _, pages := range out {
		sort.Slice(pages, func(i, j int) bool { return pages[i].index < pages[j].index })
	}
	return out, nil
}

func hashFile(path string) (string, int64, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(h, fh)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
