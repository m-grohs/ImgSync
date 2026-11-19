package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/fetch"
	"imgsync/progress"
	"imgsync/state"
)

const batchSize = 3

// Summary counts the outcome of a sync run.
type Summary struct {
	New      int
	Skipped  int
	Assumed  int
	Failed   int
	Locked   int
	Chapters int
}

// Add accumulates another summary.
func (s *Summary) Add(o Summary) {
	s.New += o.New
	s.Skipped += o.Skipped
	s.Assumed += o.Assumed
	s.Failed += o.Failed
	s.Locked += o.Locked
	s.Chapters += o.Chapters
}

// Text renders the summary line.
func (s *Summary) Text() string {
	return fmt.Sprintf(
		"+%d new, %d unchanged, %d assumed, %d locked, %d failed, %d chapters",
		s.New, s.Skipped, s.Assumed, s.Locked, s.Failed, s.Chapters,
	)
}

// AddSeries registers a series URL as a bookmark.
func AddSeries(f *fetch.Fetcher, st *state.State, rawURL string) (slug, title string, created bool, err error) {
	canonical, slug, err := discover.ParseSeriesURL(rawURL)
	if err != nil {
		return "", "", false, err
	}
	doc, err := f.GetText(canonical)
	if err != nil {
		return "", "", false, err
	}
	title = discover.SeriesTitle(doc)
	created, err = st.UpsertBookmark(slug, discover.SeriesID(slug), canonical, title)
	return slug, title, created, err
}

// AtomicWrite writes data to a .part file, fsyncs it and renames it into
// place. It returns the SHA-256 of the written data.
func AtomicWrite(dest string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp := dest + ".part"
	fh, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := fh.Write(data); err != nil {
		_ = fh.Close()
		return "", err
	}
	if err := fh.Sync(); err != nil {
		_ = fh.Close()
		return "", err
	}
	if err := fh.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func syncPage(
	f *fetch.Fetcher,
	st *state.State,
	slug, number string,
	page discover.Page,
	chapterDir string,
	dryRun, verify bool,
) string {
	dest := filepath.Join(chapterDir, fmt.Sprintf("%03d%s", page.Index+1, discover.PageExtension(page.URL)))

	row, err := st.GetPage(slug, number, page.Index)
	if err != nil {
		slog.Error("page lookup failed", "bookmark", slug, "chapter", number, "page", page.Index+1, "error", err)
		return "failed"
	}

	// The database is the single source of truth: a row means "synced".
	if row != nil && !verify {
		return "skipped"
	}

	if dryRun {
		if row != nil {
			return "skipped"
		}
		slog.Info("would download", "path", dest)
		return "new"
	}

	var img *fetch.ImageResponse
	if row != nil {
		img, err = f.FetchImage(page.URL, row.ETag, row.LastModified)
	} else {
		img, err = f.FetchImage(page.URL, "", "")
	}
	if err != nil {
		slog.Error("fetch failed", "url", page.URL, "error", err)
		return "failed"
	}
	if img == nil {
		return "skipped"
	}

	digest, err := AtomicWrite(dest, img.Body)
	if err != nil {
		slog.Error("write failed", "path", dest, "error", err)
		return "failed"
	}
	if err := st.UpsertPage(slug, number, page.Index, page.URL, dest, img.ETag, img.LastModified, digest, int64(len(img.Body))); err != nil {
		slog.Error("state update failed", "path", dest, "error", err)
		return "failed"
	}
	slog.Debug("saved", "path", dest, "bytes", len(img.Body))
	return "new"
}

func syncPages(
	f *fetch.Fetcher,
	st *state.State,
	slug, number string,
	pages []discover.Page,
	chapterDir string,
	dryRun, verify bool,
) []string {
	out := make([]string, len(pages))
	var wg stdsync.WaitGroup
	for i, page := range pages {
		wg.Add(1)
		go func(i int, page discover.Page) {
			defer wg.Done()
			out[i] = syncPage(f, st, slug, number, page, chapterDir, dryRun, verify)
		}(i, page)
	}
	wg.Wait()
	return out
}

// chapterPlan is a parsed chapter whose pages still need processing.
type chapterPlan struct {
	ref     discover.ChapterRef
	chapter *discover.Chapter
	pending int
}

// syncedIndexes groups the synced page indexes of a bookmark by chapter.
func syncedIndexes(st *state.State, slug string) (map[string]map[int]bool, error) {
	pages, err := st.PagesForBookmark(slug)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[int]bool, len(pages))
	for _, p := range pages {
		indexes := out[p.Chapter]
		if indexes == nil {
			indexes = map[int]bool{}
			out[p.Chapter] = indexes
		}
		indexes[p.Index] = true
	}
	return out, nil
}

// pendingPages counts pages that have no synced row yet.
func pendingPages(pages []discover.Page, synced map[int]bool) int {
	n := 0
	for _, page := range pages {
		if !synced[page.Index] {
			n++
		}
	}
	return n
}

// SyncSeries syncs one bookmark. outRoot is the bookmark location or the
// global output folder.
func SyncSeries(
	f *fetch.Fetcher,
	st *state.State,
	bookmark state.Bookmark,
	outRoot string,
	dryRun, verify bool,
	limit *int,
) (Summary, error) {
	summary := Summary{}
	slug := bookmark.Slug

	doc, err := f.GetText(bookmark.URL)
	if err != nil {
		return summary, err
	}
	chapters := discover.DiscoverChapters(doc)
	if len(chapters) == 0 {
		return summary, fmt.Errorf("no chapters found - the site layout may have changed")
	}

	// Chapters up to the bookmark's cutoff are trusted as already downloaded.
	// Individual exceptions (set by unsync) still download normally.
	var assumeExceptions map[string]bool
	if bookmark.AssumeSyncedThrough != "" {
		assumeExceptions, err = st.AssumeExceptions(slug)
		if err != nil {
			return summary, err
		}
	}

	// Without verification, pages already in the database are skipped locally.
	// They are not part of the download work, so keep them out of the bar total.
	needsSynced := !verify || dryRun
	var synced map[string]map[int]bool
	if needsSynced {
		synced, err = syncedIndexes(st, slug)
		if err != nil {
			return summary, err
		}
	}

	// Pass 1: index chapter pages and collect their page lists. This performs
	// the same chapter requests as the old single-pass loop and gives the
	// download bar an exact total.
	var plans []chapterPlan
	pendingTotal := 0
	indexBar := progress.New(slug, "chapters", len(chapters))
	for _, ref := range chapters {
		indexBar.SetDetail(discover.ChapterLabel(ref.Number))

		if bookmark.AssumeSyncedThrough != "" &&
			!assumeExceptions[ref.Number] &&
			discover.ChapterAtOrBefore(ref.Number, bookmark.AssumeSyncedThrough) {
			slog.Debug("chapter assumed synced", "bookmark", slug, "chapter", ref.Number)
			summary.Assumed++
			summary.Chapters++
			indexBar.Add(1)
			continue
		}

		if limit != nil && *limit <= 0 {
			slog.Info("download limit reached", "bookmark", slug)
			break
		}
		if limit != nil && needsSynced && pendingTotal >= *limit {
			slog.Info("download limit reached", "bookmark", slug)
			break
		}

		slog.Debug("indexing chapter", "bookmark", slug, "chapter", ref.Number)
		chapterURL := resolveURL(bookmark.URL, ref.Path)
		chapterDoc, err := f.GetText(chapterURL)
		if err != nil {
			slog.Error("chapter fetch failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			summary.Failed++
			indexBar.Add(1)
			continue
		}
		chapter, err := discover.ParseChapter(chapterDoc, ref.Number, chapterURL)
		if err != nil {
			slog.Error("chapter parse failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			summary.Failed++
			indexBar.Add(1)
			continue
		}

		summary.Chapters++

		if chapter.Locked {
			if err := st.SetChapter(slug, ref.Number, chapterURL, "locked", 0); err != nil {
				slog.Error("state update failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			}
			summary.Locked++
			reason := chapter.Reason
			if reason == "" {
				reason = "unknown"
			}
			slog.Info("chapter is locked - retrying next run", "bookmark", slug, "chapter", ref.Number, "reason", reason)
			indexBar.Add(1)
			continue
		}

		plan := chapterPlan{ref: ref, chapter: chapter}
		if needsSynced {
			plan.pending = pendingPages(chapter.Pages, synced[ref.Number])
		} else {
			plan.pending = len(chapter.Pages)
		}
		pendingTotal += plan.pending
		plans = append(plans, plan)
		indexBar.Add(1)
	}
	indexBar.Finish()

	total := pendingTotal
	if limit != nil && needsSynced && total > *limit {
		total = *limit
	}
	bar := progress.New(slug, "pages", total)
	defer bar.Finish()

	// Pass 2: download the planned pages in small batches.
	for _, plan := range plans {
		if limit != nil && *limit <= 0 {
			slog.Info("download limit reached", "bookmark", slug)
			break
		}
		ref := plan.ref
		chapter := plan.chapter
		bar.SetDetail(discover.ChapterLabel(ref.Number))

		if err := st.SetChapter(slug, ref.Number, chapter.URL, "ok", len(chapter.Pages)); err != nil {
			slog.Error("state update failed", "bookmark", slug, "chapter", ref.Number, "error", err)
		}
		chapterDir := filepath.Join(outRoot, discover.ChapterLabel(ref.Number))

		for start := 0; start < len(chapter.Pages); start += batchSize {
			if limit != nil && *limit <= 0 {
				break
			}
			end := start + batchSize
			if end > len(chapter.Pages) {
				end = len(chapter.Pages)
			}
			chunk := chapter.Pages[start:end]
			if limit != nil && len(chunk) > *limit {
				chunk = chunk[:*limit]
			}

			results := syncPages(f, st, slug, ref.Number, chunk, chapterDir, dryRun, verify)
			for _, result := range results {
				switch result {
				case "new":
					summary.New++
					if limit != nil {
						*limit--
					}
				case "skipped":
					summary.Skipped++
				default:
					summary.Failed++
				}
				if !needsSynced || result != "skipped" {
					bar.Add(1)
				}
			}
		}
	}

	return summary, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// relinkPagePaths updates stored page paths after a series folder is renamed.
func relinkPagePaths(st *state.State, slug, oldRoot, newRoot string) error {
	return relinkRenamedPaths(st, slug, map[string]string{oldRoot: newRoot})
}

// relinkRenamedPaths rewrites stored page paths for every renamed folder.
func relinkRenamedPaths(st *state.State, slug string, renamed map[string]string) error {
	pages, err := st.PagesForBookmark(slug)
	if err != nil {
		return err
	}
	sep := string(os.PathSeparator)
	for _, p := range pages {
		if p.Path == "" {
			continue
		}
		for oldRoot, newRoot := range renamed {
			oldPrefix := oldRoot + sep
			if !strings.HasPrefix(p.Path, oldPrefix) {
				continue
			}
			next := newRoot + sep + strings.TrimPrefix(p.Path, oldPrefix)
			if err := st.SetPagePath(slug, p.Chapter, p.Index, next); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

// seriesOutRoot is the download folder for a bookmark without an explicit
// location. Folders created by older versions from the lowercase URL slug are
// renamed to the title-based name once, so downloads stay in one folder.
func seriesOutRoot(st *state.State, b state.Bookmark, dryRun bool) string {
	base := config.OutputDir()
	target := filepath.Join(base, discover.SeriesFolderName(b.Title, b.Slug))
	legacy := filepath.Join(base, discover.SeriesFolder(b.Slug))
	if target == legacy {
		return target
	}

	targetExists := isDir(target)
	legacyExists := isDir(legacy)
	switch {
	case !targetExists && legacyExists:
		if dryRun {
			return legacy
		}
		if err := os.Rename(legacy, target); err != nil {
			slog.Warn(
				"cannot rename series folder to title - keeping the existing folder",
				"bookmark", b.Slug, "from", legacy, "to", target, "error", err,
			)
			return legacy
		}
		if err := relinkPagePaths(st, b.Slug, legacy, target); err != nil {
			slog.Warn("series folder renamed but stored page paths were not updated", "bookmark", b.Slug, "error", err)
		} else {
			slog.Info("series folder renamed to title", "bookmark", b.Slug, "from", legacy, "to", target)
		}
	case targetExists && legacyExists:
		slog.Warn(
			"both title-based and slug-based series folders exist - using the title-based one",
			"bookmark", b.Slug, "title_folder", target, "slug_folder", legacy,
		)
	}
	return target
}

// normalizeChapterFolders renames legacy lowercase chapter folders to the
// canonical "Chapter NNN" form and updates their stored page paths.
func normalizeChapterFolders(st *state.State, slug, outRoot string, dryRun bool) error {
	entries, err := os.ReadDir(outRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	renamed := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		number, ok := discover.ParseChapterFolderName(entry.Name())
		if !ok {
			continue
		}
		canonical := discover.ChapterLabel(number)
		if entry.Name() == canonical {
			continue
		}

		oldDir := filepath.Join(outRoot, entry.Name())
		newDir := filepath.Join(outRoot, canonical)
		if newInfo, err := os.Stat(newDir); err == nil {
			oldInfo, statErr := os.Stat(oldDir)
			if statErr != nil || !os.SameFile(newInfo, oldInfo) {
				slog.Warn(
					"both legacy and canonical chapter folders exist - keeping both",
					"bookmark", slug, "legacy_folder", oldDir, "canonical_folder", newDir,
				)
				continue
			}
		}

		if dryRun {
			slog.Info("would rename chapter folder", "bookmark", slug, "from", oldDir, "to", newDir)
			continue
		}
		if err := os.Rename(oldDir, newDir); err != nil {
			slog.Warn("cannot rename chapter folder", "bookmark", slug, "from", oldDir, "to", newDir, "error", err)
			continue
		}
		renamed[oldDir] = newDir
		slog.Info("chapter folder renamed", "bookmark", slug, "from", oldDir, "to", newDir)
	}

	if len(renamed) > 0 {
		if err := relinkRenamedPaths(st, slug, renamed); err != nil {
			slog.Warn("chapter folders renamed but stored page paths were not updated", "bookmark", slug, "error", err)
		}
	}
	return nil
}

// SyncAll syncs every enabled bookmark (optionally filtered by slug).
func SyncAll(
	f *fetch.Fetcher,
	st *state.State,
	slugs []string,
	dryRun, verify bool,
	limit *int,
) (Summary, []string, error) {
	bookmarks, err := st.EnabledBookmarks()
	if err != nil {
		return Summary{}, nil, err
	}

	if len(slugs) > 0 {
		wanted := map[string]bool{}
		for _, s := range slugs {
			wanted[s] = true
		}
		known := map[string]bool{}
		for _, b := range bookmarks {
			known[b.Slug] = true
		}
		for _, s := range slugs {
			if !known[s] {
				return Summary{}, nil, fmt.Errorf("unknown or disabled bookmark: %s", s)
			}
		}
		filtered := bookmarks[:0]
		for _, b := range bookmarks {
			if wanted[b.Slug] {
				filtered = append(filtered, b)
			}
		}
		bookmarks = filtered
	}

	if len(bookmarks) == 0 {
		return Summary{}, nil, fmt.Errorf("no bookmarks registered - add one with: imgsync add <url>")
	}

	totals := Summary{}
	var failures []string

	for _, b := range bookmarks {
		started := state.Now()
		outRoot := b.Location
		if outRoot == "" {
			outRoot = seriesOutRoot(st, b, dryRun)
		}
		if err := normalizeChapterFolders(st, b.Slug, outRoot, dryRun); err != nil {
			slog.Warn("chapter folder migration failed", "bookmark", b.Slug, "error", err)
		}

		slog.Info("syncing", "bookmark", b.Slug)
		summary, err := SyncSeries(f, st, b, outRoot, dryRun, verify, limit)

		result := "ok"
		note := ""
		newPages, skipped, failed, locked := 0, 0, 0, 0

		if err != nil {
			failed = 1
			totals.Failed++
			result = "error"
			note = "error: " + err.Error()
			failures = append(failures, b.Slug)
			slog.Error("bookmark failed", "bookmark", b.Slug, "error", err)
		} else {
			totals.Add(summary)
			newPages, skipped, failed, locked = summary.New, summary.Skipped, summary.Failed, summary.Locked
			note = summary.Text()
			if summary.Failed > 0 {
				result = "partial"
				failures = append(failures, b.Slug)
			}
			slog.Info("synced", "bookmark", b.Slug, "note", note)
		}

		if err := st.RecordRun(b.Slug, started, state.Now(), newPages, skipped, failed, locked, note); err != nil {
			slog.Error("failed to record run", "bookmark", b.Slug, "error", err)
		}
		if err := st.RecordBookmarkRun(b.Slug, result); err != nil {
			slog.Error("failed to record bookmark run", "bookmark", b.Slug, "error", err)
		}
	}

	return totals, failures, nil
}
