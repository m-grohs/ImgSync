package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	stdsync "sync"

	"imgsync/config"
	"imgsync/discover"
	"imgsync/fetch"
	"imgsync/state"
)

const batchSize = 3

// Summary counts the outcome of a sync run.
type Summary struct {
	New      int
	Skipped  int
	Failed   int
	Locked   int
	Chapters int
}

// Add accumulates another summary.
func (s *Summary) Add(o Summary) {
	s.New += o.New
	s.Skipped += o.Skipped
	s.Failed += o.Failed
	s.Locked += o.Locked
	s.Chapters += o.Chapters
}

// Text renders the summary line.
func (s *Summary) Text() string {
	return fmt.Sprintf(
		"+%d new, %d unchanged, %d locked, %d failed, %d chapters",
		s.New, s.Skipped, s.Locked, s.Failed, s.Chapters,
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

	for _, ref := range chapters {
		if limit != nil && *limit <= 0 {
			slog.Info("download limit reached", "bookmark", slug)
			break
		}

		chapterURL := resolveURL(bookmark.URL, ref.Path)
		chapterDoc, err := f.GetText(chapterURL)
		if err != nil {
			slog.Error("chapter fetch failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			summary.Failed++
			continue
		}
		chapter, err := discover.ParseChapter(chapterDoc, ref.Number, chapterURL)
		if err != nil {
			slog.Error("chapter parse failed", "bookmark", slug, "chapter", ref.Number, "error", err)
			summary.Failed++
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
			continue
		}

		if err := st.SetChapter(slug, ref.Number, chapterURL, "ok", len(chapter.Pages)); err != nil {
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
			}
		}
	}

	return summary, nil
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
			outRoot = filepath.Join(config.OutputDir(), discover.SeriesFolder(b.Slug))
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
