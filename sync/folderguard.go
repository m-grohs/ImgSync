package sync

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/state"
)

// folderConflict describes an existing series folder that has no pages of the
// bookmark recorded inside it, so writing there may replace unrelated files.
type folderConflict struct {
	slug     string
	path     string
	chapters int
	files    int
}

// seriesCandidatePaths returns the title-based folder and the legacy
// slug-derived folder for a bookmark.
func seriesCandidatePaths(b state.Bookmark) (target, legacy string, err error) {
	base, err := config.OutputDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(base, discover.SeriesFolderName(b.Title, b.Slug)),
		filepath.Join(base, discover.SeriesFolder(b.Slug)), nil
}

// findSeriesFolderConflict reports an existing, non-empty series folder that is
// not associated with the bookmark. Bookmarks with an explicit location are
// exempt: set-location has already integrated their files into the database.
func findSeriesFolderConflict(st *state.State, b state.Bookmark) (*folderConflict, error) {
	target, legacy, err := seriesCandidatePaths(b)
	if err != nil {
		return nil, err
	}

	owned, err := folderOwned(st, b.Slug, target, legacy)
	if err != nil {
		return nil, err
	}
	if owned {
		return nil, nil
	}

	for _, path := range []string{target, legacy} {
		if path == "" || !isDir(path) {
			continue
		}
		chapters, files := folderContents(path)
		if chapters == 0 && files == 0 {
			continue
		}
		return &folderConflict{slug: b.Slug, path: path, chapters: chapters, files: files}, nil
	}
	return nil, nil
}

// folderOwned reports whether any synced page of the bookmark is stored under
// one of the candidate folders. That makes a normal incremental sync silent;
// only folders with no database connection are treated as conflicts.
func folderOwned(st *state.State, slug string, roots ...string) (bool, error) {
	pages, err := st.PagesForBookmark(slug)
	if err != nil {
		return false, err
	}
	for _, p := range pages {
		if p.Path == "" {
			continue
		}
		for _, root := range roots {
			if withinDir(p.Path, root) {
				return true, nil
			}
		}
	}
	// unsync deletes page rows but keeps chapter rows. The folder still belongs
	// to this bookmark, and re-downloading into it is intentional.
	chapters, err := st.AllChaptersForBookmark(slug)
	if err != nil {
		return false, err
	}
	return len(chapters) > 0, nil
}

// withinDir reports whether path is inside dir, comparing clean absolute paths.
func withinDir(path, dir string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// folderContents counts chapter folders and loose files in an existing folder.
func folderContents(path string) (chapters, files int) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, 0
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".imgsync-") {
			continue
		}
		if entry.IsDir() {
			chapters++
		} else {
			files++
		}
	}
	return chapters, files
}

func (c *folderConflict) contents() string {
	parts := make([]string, 0, 2)
	if c.chapters > 0 {
		parts = append(parts, fmt.Sprintf("%d chapter %s", c.chapters, plural(c.chapters, "folder", "folders")))
	}
	if c.files > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", c.files, plural(c.files, "file", "files")))
	}
	return strings.Join(parts, ", ")
}

// confirmFolderUse asks once whether the existing folder may be used. A
// non-interactive run without --yes fails closed instead of guessing.
func confirmFolderUse(c *folderConflict, assumeYes, interactive bool, in io.Reader) (bool, error) {
	fmt.Printf("series folder for %s already exists and is not linked to this bookmark:\n", c.slug)
	fmt.Printf("  %s\n", c.path)
	if contents := c.contents(); contents != "" {
		fmt.Printf("  %s\n", contents)
	}
	if assumeYes {
		fmt.Println("using it (--yes): files at the same destination path may be overwritten")
		return true, nil
	}
	if !interactive {
		return false, fmt.Errorf(
			"existing series folder %s is not linked to this bookmark; rerun with --yes to use it, or set-location to adopt it",
			c.path,
		)
	}

	fmt.Print("Use it anyway? Files at the same destination path may be overwritten. [y/N] ")
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func isInteractive(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
