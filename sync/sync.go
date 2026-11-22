package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"

	"imgsync/discover"
	"imgsync/fetch"
	"imgsync/progress"
	"imgsync/state"
)

const batchSize = 3

// Summary counts the outcome of a sync run. Failure counters are kept separate
// by unit so a run can report one number without mixing pages and chapters.
type Summary struct {
	New             int      // pages downloaded
	Skipped         int      // pages that were already present
	Assumed         int      // chapters skipped by the bookmark's cutoff
	Synced          int      // chapters skipped because they were fully synced
	Failed          int      // pages that failed
	FailedChapters  int      // chapters that failed to fetch or parse
	FailedBookmarks int      // bookmarks that failed as a whole
	Locked          int      // locked chapters
	Chapters        int      // chapters considered
	NewChapters     []string // chapters that received at least one new page
}

// Failures returns the total number of failed units.
func (s *Summary) Failures() int { return s.Failed + s.FailedChapters + s.FailedBookmarks }

// Add accumulates another summary.
func (s *Summary) Add(o Summary) {
	s.New += o.New
	s.Skipped += o.Skipped
	s.Assumed += o.Assumed
	s.Synced += o.Synced
	s.Failed += o.Failed
	s.FailedChapters += o.FailedChapters
	s.FailedBookmarks += o.FailedBookmarks
	s.Locked += o.Locked
	s.Chapters += o.Chapters
	s.NewChapters = append(s.NewChapters, o.NewChapters...)
}

// Text renders the summary line for the whole run. Chapter ranges only make
// sense within one series, so the aggregate line reports counts instead.
func (s *Summary) Text() string {
	parts := []string{s.head(false)}
	return strings.Join(append(parts, s.details()...), ", ")
}

// Note renders the summary of one bookmark, including the downloaded chapter
// range (for example "Chapter 122-125").
func (s *Summary) Note() string {
	parts := []string{s.head(true)}
	return strings.Join(append(parts, s.details()...), ", ")
}

func (s *Summary) head(withRange bool) string {
	if len(s.NewChapters) == 0 {
		if s.New == 0 {
			return "no new pages"
		}
		return fmt.Sprintf("+%d %s", s.New, plural(s.New, "page", "pages"))
	}
	chapters := fmt.Sprintf("%d %s", len(s.NewChapters), plural(len(s.NewChapters), "chapter", "chapters"))
	if withRange {
		return fmt.Sprintf("+%s (%s), %d %s",
			chapters, discover.ChapterRangeLabel(s.NewChapters), s.New, plural(s.New, "page", "pages"))
	}
	return fmt.Sprintf("+%s (%d %s)", chapters, s.New, plural(s.New, "page", "pages"))
}

func (s *Summary) details() []string {
	var parts []string
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d %s unchanged", s.Skipped, plural(s.Skipped, "page", "pages")))
	}
	if s.Assumed > 0 {
		parts = append(parts, fmt.Sprintf("%d %s assumed", s.Assumed, plural(s.Assumed, "chapter", "chapters")))
	}
	if s.Synced > 0 {
		parts = append(parts, fmt.Sprintf("%d %s up to date", s.Synced, plural(s.Synced, "chapter", "chapters")))
	}
	if s.Locked > 0 {
		parts = append(parts, fmt.Sprintf("%d locked", s.Locked))
	}
	if n := s.Failures(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", n))
	}
	return parts
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// BookmarkResult is the outcome of syncing one bookmark.
type BookmarkResult struct {
	Bookmark state.Bookmark
	Summary  Summary
	Skipped  bool
	SkipNote string
	Err      error
}

// Result is the outcome of a sync run, including one entry per bookmark that
// was reached before the run finished or was interrupted.
type Result struct {
	Totals    Summary
	Bookmarks []BookmarkResult
}

// Failures returns the slugs of bookmarks that failed or had partial failures.
func (r Result) Failures() []string {
	var out []string
	for _, b := range r.Bookmarks {
		if b.Err != nil || b.Summary.Failures() > 0 {
			out = append(out, b.Bookmark.Slug)
		}
	}
	return out
}

// AddSeries registers a series URL as a bookmark.
func AddSeries(ctx context.Context, f *fetch.Fetcher, st *state.State, rawURL string) (slug, title string, created bool, err error) {
	canonical, slug, err := discover.ParseSeriesURL(rawURL)
	if err != nil {
		return "", "", false, err
	}
	doc, err := f.GetText(ctx, canonical)
	if err != nil {
		return "", "", false, err
	}
	title = discover.SeriesTitle(doc)
	created, err = st.UpsertBookmark(slug, discover.SeriesID(slug), canonical, title)
	return slug, title, created, err
}

// AtomicWrite writes data to a unique temporary file, fsyncs it and renames
// it into place. No stale .part file is ever left behind. It returns the
// SHA-256 of the written data.
func AtomicWrite(dest string, data []byte) (string, error) {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fh, err := os.CreateTemp(dir, ".imgsync-*.part")
	if err != nil {
		return "", err
	}
	tmp := fh.Name()
	defer func() { _ = os.Remove(tmp) }()

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
	if err := syncDir(dir); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// syncDir makes the rename durable where the platform supports it.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		// Some filesystems do not support fsync on directories.
		slog.Warn("cannot fsync directory", "dir", dir, "error", err)
	}
	return nil
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
	ctx context.Context,
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
		if ctx.Err() == nil {
			slog.Error("page lookup failed", "bookmark", slug, "chapter", number, "page", page.Index+1, "error", err)
		}
		return "failed"
	}

	// The database is the single source of truth: a row means "synced".
	if row != nil && !verify {
		return "skipped"
	}
	// Pages integrated by set-location have no source URL. There is nothing to
	// revalidate, so --verify must not turn them into failures.
	if verify && row != nil && row.URL == "" {
		return "skipped"
	}
	// A dry run without verification reports purely from the database. With
	// verification, the conditional GET below is needed to detect changes.
	if dryRun && row == nil {
		slog.Info("would download", "path", dest)
		return "new"
	}

	var img *fetch.ImageResponse
	if row != nil {
		img, err = f.FetchImage(ctx, page.URL, row.ETag, row.LastModified)
	} else {
		img, err = f.FetchImage(ctx, page.URL, "", "")
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("fetch failed", "url", page.URL, "error", err)
		}
		return "failed"
	}
	if img == nil {
		return "skipped"
	}
	if dryRun {
		slog.Info("would download", "path", dest)
		return "new"
	}

	sum := sha256.Sum256(img.Body)
	digest := hex.EncodeToString(sum[:])
	if row != nil && row.SHA256 == digest {
		// A 200 with identical bytes is unchanged: refresh validators but keep
		// the stored path and do not rewrite the file or count a new page.
		if err := st.UpsertPage(slug, number, page.Index, page.URL, row.Path, img.ETag, img.LastModified, digest, int64(len(img.Body))); err != nil {
			slog.Error("state update failed", "path", row.Path, "error", err)
		}
		return "skipped"
	}

	if _, err := AtomicWrite(dest, img.Body); err != nil {
		if ctx.Err() == nil {
			slog.Error("write failed", "path", dest, "error", err)
		}
		return "failed"
	}
	if old := supersededPath(row, dest, chapterDir, page.Index); old != "" {
		if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("old page file left behind", "path", old, "error", err)
		}
	}
	if err := st.UpsertPage(slug, number, page.Index, page.URL, dest, img.ETag, img.LastModified, digest, int64(len(img.Body))); err != nil {
		slog.Error("state update failed", "path", dest, "error", err)
		return "failed"
	}
	slog.Debug("saved", "path", dest, "bytes", len(img.Body))
	return "new"
}

// supersededPath finds a file from an earlier run whose extension no longer
// matches the current URL, so it can be removed after the new file is written.
func supersededPath(row *state.Page, dest, chapterDir string, index int) string {
	if row == nil || row.Path == "" || row.Path == dest {
		return ""
	}
	if filepath.Dir(row.Path) != chapterDir {
		return ""
	}
	if !strings.HasPrefix(filepath.Base(row.Path), fmt.Sprintf("%03d.", index+1)) {
		return ""
	}
	return row.Path
}

func syncPages(
	ctx context.Context,
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
			out[i] = syncPage(ctx, f, st, slug, number, page, chapterDir, dryRun, verify)
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

// discoverSeries fetches a series page and returns the chapters that still
// need to be fetched. It runs before any chapter work so a progress bar can
// count the chapters that will actually be requested.
func discoverSeries(
	ctx context.Context,
	f *fetch.Fetcher,
	st *state.State,
	bookmark state.Bookmark,
	verify bool,
) ([]discover.ChapterRef, Summary, error) {
	summary := Summary{}
	slug := bookmark.Slug

	if err := ctx.Err(); err != nil {
		return nil, summary, err
	}
	doc, err := f.GetText(ctx, bookmark.URL)
	if err != nil {
		return nil, summary, err
	}
	chapters := discover.DiscoverChapters(doc)
	if len(chapters) == 0 {
		return nil, summary, fmt.Errorf("no chapters found - the site layout may have changed")
	}

	// Chapters up to the bookmark's cutoff are trusted as already downloaded.
	// Individual exceptions (set by unsync) still download normally.
	var assumeExceptions map[string]bool
	if bookmark.AssumeSyncedThrough != "" {
		assumeExceptions, err = st.AssumeExceptions(slug)
		if err != nil {
			return nil, summary, err
		}
	}

	// Chapters that were completely downloaded before are not fetched during
	// indexing. --verify re-indexes everything to catch changed pages.
	var syncedChapters map[string]bool
	if !verify {
		syncedChapters, err = st.SyncedChapters(slug)
		if err != nil {
			return nil, summary, err
		}
	}

	refs := make([]discover.ChapterRef, 0, len(chapters))
	for _, ref := range chapters {
		if bookmark.AssumeSyncedThrough != "" &&
			!assumeExceptions[ref.Number] &&
			discover.ChapterAtOrBefore(ref.Number, bookmark.AssumeSyncedThrough) {
			slog.Debug("chapter assumed synced", "bookmark", slug, "chapter", ref.Number)
			summary.Assumed++
			summary.Chapters++
			continue
		}

		if syncedChapters[ref.Number] {
			slog.Debug("chapter up to date", "bookmark", slug, "chapter", ref.Number)
			summary.Synced++
			summary.Chapters++
			continue
		}

		refs = append(refs, ref)
	}
	return refs, summary, nil
}

// indexSeries fetches and parses the chapters returned by discoverSeries and
// builds download plans for the whole run. It performs no page downloads.
// onChapter is called once per chapter page request; onRemaining is called
// with the number of candidates left unrequested when a limit stops the loop.
func indexSeries(
	ctx context.Context,
	f *fetch.Fetcher,
	st *state.State,
	bookmark state.Bookmark,
	refs []discover.ChapterRef,
	dryRun, verify bool,
	limit *int,
	onChapter func(),
	onRemaining func(int),
) ([]chapterPlan, Summary, error) {
	summary := Summary{}
	slug := bookmark.Slug

	// Without verification, pages already in the database are skipped locally.
	// They are not part of the download work, so keep them out of the bar total.
	needsSynced := !verify || dryRun
	var synced map[string]map[int]bool
	if needsSynced {
		var err error
		synced, err = syncedIndexes(st, slug)
		if err != nil {
			return nil, summary, err
		}
	}

	var plans []chapterPlan
	pendingTotal := 0
	for i, ref := range refs {
		if err := ctx.Err(); err != nil {
			return plans, summary, err
		}
		if limit != nil && *limit <= 0 {
			slog.Info("download limit reached", "bookmark", slug)
			onRemaining(len(refs) - i)
			break
		}
		if limit != nil && needsSynced && pendingTotal >= *limit {
			slog.Info("download limit reached", "bookmark", slug)
			onRemaining(len(refs) - i)
			break
		}

		slog.Debug("indexing chapter", "bookmark", slug, "chapter", ref.Number)
		chapterURL := resolveURL(bookmark.URL, ref.Path)
		chapterDoc, err := f.GetText(ctx, chapterURL)
		onChapter()
		if err != nil {
			if ctx.Err() != nil {
				return plans, summary, ctx.Err()
			}
			slog.Error("chapter fetch failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			summary.FailedChapters++
			summary.Chapters++
			continue
		}
		chapter, err := discover.ParseChapter(chapterDoc, ref.Number, chapterURL)
		if err != nil {
			slog.Error("chapter parse failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			summary.FailedChapters++
			summary.Chapters++
			continue
		}

		summary.Chapters++

		if chapter.Locked {
			if err := st.SetChapter(slug, ref.Number, chapterURL, state.ChapterStatusLocked, 0); err != nil {
				slog.Error("state update failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			}
			summary.Locked++
			reason := chapter.Reason
			if reason == "" {
				reason = "unknown"
			}
			slog.Info("chapter is locked - retrying next run", "bookmark", slug, "chapter", ref.Number, "reason", reason)
			continue
		}

		if chapter.Empty {
			// No payload means the parser could not read the chapter; it is
			// not a paywall. Report it without persisting a "locked" status.
			slog.Warn("chapter has no pages - site layout may have changed", "bookmark", slug, "chapter", ref.Number)
			summary.FailedChapters++
			continue
		}

		plan := chapterPlan{ref: ref, chapter: chapter}
		if needsSynced {
			plan.pending = pendingPages(chapter.Pages, synced[ref.Number])
		} else {
			plan.pending = len(chapter.Pages)
		}

		// Everything is already in the database. Remember it so later runs can
		// skip this chapter page entirely.
		if !verify && needsSynced && plan.pending == 0 && len(chapter.Pages) > 0 {
			if !dryRun {
				if err := st.SetChapter(slug, ref.Number, chapterURL, state.ChapterStatusSynced, len(chapter.Pages)); err != nil {
					slog.Error("state update failed", "bookmark", slug, "chapter", ref.Number, "error", err)
				}
			}
			slog.Debug("chapter up to date", "bookmark", slug, "chapter", ref.Number)
			summary.Synced++
			continue
		}

		pendingTotal += plan.pending
		plans = append(plans, plan)
	}
	return plans, summary, nil
}

// displayName is the human-readable series name used on progress lines.
func displayName(b state.Bookmark) string {
	if b.Title != "" {
		return b.Title
	}
	return b.Slug
}

// downloadSeries downloads the pages of every plan under one live bar per
// series. The bar counts pages and switches its detail to the current chapter.
func downloadSeries(
	ctx context.Context,
	f *fetch.Fetcher,
	st *state.State,
	bookmark state.Bookmark,
	outRoot string,
	plans []chapterPlan,
	dryRun, verify bool,
	limit *int,
) (Summary, error) {
	summary := Summary{}
	slug := bookmark.Slug
	needsSynced := !verify || dryRun

	total := 0
	for _, plan := range plans {
		if needsSynced {
			total += plan.pending
		} else {
			total += len(plan.chapter.Pages)
		}
	}
	if limit != nil && needsSynced && total > *limit {
		total = *limit
	}
	bar := progress.New(displayName(bookmark), "", "pages", total)
	defer bar.Clear()

	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if limit != nil && *limit <= 0 {
			break
		}
		ref := plan.ref
		chapter := plan.chapter
		bar.SetDetail(discover.ChapterLabel(ref.Number))

		if err := st.SetChapter(slug, ref.Number, chapter.URL, state.ChapterStatusOK, len(chapter.Pages)); err != nil {
			slog.Error("state update failed", "bookmark", slug, "chapter", ref.Number, "error", err)
		}
		chapterDir := filepath.Join(outRoot, discover.ChapterLabel(ref.Number))

		handled, chapterFailed, chapterNew := 0, 0, 0
		for start := 0; start < len(chapter.Pages); start += batchSize {
			if err := ctx.Err(); err != nil {
				return summary, err
			}
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

			results := syncPages(ctx, f, st, slug, ref.Number, chunk, chapterDir, dryRun, verify)
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			handled += len(results)
			for _, result := range results {
				switch result {
				case "new":
					summary.New++
					chapterNew++
					if limit != nil {
						*limit--
					}
				case "skipped":
					summary.Skipped++
				default:
					summary.Failed++
					chapterFailed++
				}
				// With --verify and no limit, unchanged pages are part of the
				// work (they were checked with a conditional GET). When a
				// limit is set the bar is capped at new pages only.
				if result != "skipped" || (!needsSynced && limit == nil) {
					bar.Add(1)
				}
			}
		}

		if chapterNew > 0 {
			summary.NewChapters = append(summary.NewChapters, ref.Number)
		}

		// Remember a chapter as fully synced only when every page was handled
		// and none failed (a download limit or an error leaves it pending).
		if !dryRun && chapterFailed == 0 && handled == len(chapter.Pages) && len(chapter.Pages) > 0 {
			if err := st.SetChapter(slug, ref.Number, chapter.URL, state.ChapterStatusSynced, len(chapter.Pages)); err != nil {
				slog.Error("state update failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			} else {
				slog.Debug("chapter synced", "bookmark", slug, "chapter", ref.Number, "pages", len(chapter.Pages))
			}
		}
	}

	return summary, nil
}

// SyncSeries syncs one bookmark. outRoot is the bookmark location or the
// global output folder. SyncAll uses discoverSeries, indexSeries and
// downloadSeries directly so every series can be indexed before any download.
func SyncSeries(
	ctx context.Context,
	f *fetch.Fetcher,
	st *state.State,
	bookmark state.Bookmark,
	outRoot string,
	dryRun, verify bool,
	limit *int,
) (Summary, error) {
	refs, summary, err := discoverSeries(ctx, f, st, bookmark, verify)
	if err != nil {
		return summary, err
	}
	plans, indexed, err := indexSeries(
		ctx, f, st, bookmark, refs, dryRun, verify, limit,
		func() {}, func(int) {},
	)
	summary.Add(indexed)
	if err != nil {
		return summary, err
	}
	downloaded, err := downloadSeries(ctx, f, st, bookmark, outRoot, plans, dryRun, verify, limit)
	summary.Add(downloaded)
	return summary, err
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
func seriesOutRoot(st *state.State, b state.Bookmark, dryRun bool) (string, error) {
	target, legacy, err := seriesCandidatePaths(b)
	if err != nil {
		return "", err
	}
	if target == legacy {
		return target, nil
	}

	targetExists := isDir(target)
	legacyExists := isDir(legacy)
	switch {
	case !targetExists && legacyExists:
		if dryRun {
			return legacy, nil
		}
		if err := os.Rename(legacy, target); err != nil {
			slog.Warn(
				"cannot rename series folder to title - keeping the existing folder",
				"bookmark", b.Slug, "from", legacy, "to", target, "error", err,
			)
			return legacy, nil
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
	return target, nil
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

// Options controls one sync run.
type Options struct {
	Slugs       []string
	DryRun      bool
	Verify      bool
	AssumeYes   bool      // accept existing-folder prompts without asking
	Limit       *int      // max new pages per bookmark
	Input       io.Reader // confirmation input; nil means os.Stdin (TTY-detected)
	Interactive *bool     // overrides TTY detection; nil means auto-detect
}

// SyncAll syncs every enabled bookmark (optionally filtered by slug). The
// page limit is applied independently to each bookmark.
func SyncAll(ctx context.Context, f *fetch.Fetcher, st *state.State, opts Options) (Result, error) {
	result := Result{}
	bookmarks, err := st.EnabledBookmarks()
	if err != nil {
		return result, err
	}

	if len(opts.Slugs) > 0 {
		wanted := map[string]bool{}
		for _, s := range opts.Slugs {
			wanted[s] = true
		}
		known := map[string]bool{}
		for _, b := range bookmarks {
			known[b.Slug] = true
		}
		for _, s := range opts.Slugs {
			if !known[s] {
				return result, fmt.Errorf("unknown or disabled bookmark: %s", s)
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
		return result, fmt.Errorf("no bookmarks registered - add one with: imgsync add <url>")
	}

	type seriesRun struct {
		bookmark state.Bookmark
		outRoot  string
		started  string
		limit    *int
		refs     []discover.ChapterRef
		plans    []chapterPlan
		summary  Summary
		skip     bool
		skipNote string
		err      error
	}

	runs := make([]seriesRun, 0, len(bookmarks))
	for _, b := range bookmarks {
		run := seriesRun{bookmark: b, started: state.Now()}
		if opts.Limit != nil {
			n := *opts.Limit
			run.limit = &n
		}
		runs = append(runs, run)
	}

	// Decide about existing series folders before starting any progress bar,
	// folder rename or network work. An incremental sync is silent because its
	// folder already has page rows in the database.
	input := opts.Input
	interactive := true
	if input == nil {
		input = os.Stdin
		interactive = isInteractive(input)
	}
	if opts.Interactive != nil {
		interactive = *opts.Interactive
	}
	for i := range runs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		run := &runs[i]
		if run.bookmark.Location != "" {
			continue
		}
		conflict, err := findSeriesFolderConflict(st, run.bookmark)
		if err != nil {
			slog.Warn("cannot check series folder", "bookmark", run.bookmark.Slug, "error", err)
			continue
		}
		if conflict == nil {
			continue
		}
		if opts.DryRun {
			slog.Warn("existing series folder would need confirmation",
				"bookmark", run.bookmark.Slug, "path", conflict.path)
			continue
		}
		use, err := confirmFolderUse(conflict, opts.AssumeYes, interactive, input)
		if err != nil {
			run.skip, run.err = true, err
			continue
		}
		if !use {
			run.skip = true
			run.skipNote = "skipped: existing series folder not confirmed"
		}
	}

	// First discover every series page. The short-lived bar shows progress
	// while the total number of chapters is still unknown.
	discoveryBar := progress.New("Indexing", "", "series", len(runs))
	defer discoveryBar.Clear()
	for i := range runs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		run := &runs[i]
		if run.skip {
			discoveryBar.Add(1)
			continue
		}
		b := run.bookmark
		run.outRoot = b.Location
		if run.outRoot == "" {
			run.outRoot, err = seriesOutRoot(st, b, opts.DryRun)
			if err != nil {
				return result, err
			}
		}
		if err := normalizeChapterFolders(st, b.Slug, run.outRoot, opts.DryRun); err != nil {
			slog.Warn("chapter folder migration failed", "bookmark", b.Slug, "error", err)
		}

		slog.Info("syncing", "bookmark", b.Slug)
		run.refs, run.summary, run.err = discoverSeries(ctx, f, st, b, opts.Verify)
		discoveryBar.Add(1)
	}
	discoveryBar.Clear()

	// The chapter bar counts only chapters whose page is actually requested.
	// --limit can shorten a series, so the total shrinks when that happens.
	totalChapters := 0
	for i := range runs {
		if runs[i].err == nil && !runs[i].skip {
			totalChapters += len(runs[i].refs)
		}
	}
	indexBar := progress.New("Indexing", "", "chapters", totalChapters)
	defer indexBar.Clear()
	for i := range runs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		run := &runs[i]
		if run.err != nil || run.skip {
			continue
		}
		indexBar.SetDetail(displayName(run.bookmark))
		plans, indexed, err := indexSeries(
			ctx, f, st, run.bookmark, run.refs, opts.DryRun, opts.Verify, run.limit,
			func() { indexBar.Add(1) },
			func(n int) {
				totalChapters -= n
				indexBar.SetTotal(totalChapters)
			},
		)
		run.plans = plans
		run.summary.Add(indexed)
		run.err = err
	}
	indexBar.SetDetail("")
	indexBar.Clear()

	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			break
		}
		b := run.bookmark
		summary := run.summary
		err := run.err
		item := BookmarkResult{
			Bookmark: b,
			Summary:  summary,
			Skipped:  run.skip,
			SkipNote: run.skipNote,
			Err:      err,
		}
		if !run.skip && err == nil {
			var downloaded Summary
			downloaded, err = downloadSeries(ctx, f, st, b, run.outRoot, run.plans, opts.DryRun, opts.Verify, run.limit)
			// Keep pages that finished before a cancellation or error so the
			// interrupt summary can report saved progress accurately.
			summary.Add(downloaded)
			item.Summary = summary
			item.Err = err
		}

		outcome := "ok"
		note := ""
		newPages, skipped, failed, locked := 0, 0, 0, 0

		switch {
		case item.Skipped && err == nil:
			outcome = "skipped"
			note = item.SkipNote
			slog.Info("bookmark skipped", "bookmark", b.Slug, "note", note)
		case err != nil:
			result.Totals.Add(item.Summary)
			newPages, skipped, failed, locked = item.Summary.New, item.Summary.Skipped, item.Summary.Failures(), item.Summary.Locked
			failed++
			result.Totals.FailedBookmarks++
			outcome = "error"
			note = "error: " + err.Error()
			if ctx.Err() == nil {
				slog.Error("bookmark failed", "bookmark", b.Slug, "error", err)
			}
		default:
			result.Totals.Add(item.Summary)
			newPages, skipped, failed, locked = item.Summary.New, item.Summary.Skipped, item.Summary.Failures(), item.Summary.Locked
			note = item.Summary.Note()
			if item.Summary.Failures() > 0 {
				outcome = "partial"
			}
			slog.Info("synced", "bookmark", b.Slug, "note", note)
		}

		if ctx.Err() != nil {
			outcome = "interrupted"
			note = item.Summary.Note()
		}
		if err := st.RecordRun(b.Slug, run.started, state.Now(), newPages, skipped, failed, locked, note); err != nil {
			slog.Error("failed to record run", "bookmark", b.Slug, "error", err)
		}
		if err := st.RecordBookmarkRun(b.Slug, outcome); err != nil {
			slog.Error("failed to record bookmark run", "bookmark", b.Slug, "error", err)
		}
		result.Bookmarks = append(result.Bookmarks, item)
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}
