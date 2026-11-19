package state

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"imgsync/discover"

	_ "modernc.org/sqlite"
)

// State wraps the SQLite state database.
type State struct {
	db *sql.DB
}

// Bookmark is a registered series ("bookmark").
type Bookmark struct {
	Slug       string
	SeriesID   string
	URL        string
	Title      string
	Location   string
	Enabled    bool
	AddedAt    string
	LastRun    string
	LastResult string
	Chapters   int
	Pages      int
}

// Page is a synced page row.
type Page struct {
	Slug         string
	Chapter      string
	Index        int
	URL          string
	Path         string
	ETag         string
	LastModified string
	SHA256       string
	Size         int64
	Status       string
	UpdatedAt    string
}

// Now returns the current UTC timestamp in the stored format.
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05-07:00") }

// Open opens (and initializes) the state database. It refuses to touch a
// legacy Python database (which has a "series" table) so nothing is corrupted.
func Open(path string) (*State, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	legacy, err := hasLegacySchema(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if legacy {
		_ = db.Close()
		return nil, fmt.Errorf(
			"legacy Python database found at %s: move it away so ImgSync can start fresh (nothing has been changed)",
			path,
		)
	}

	s := &State{db: db}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return s, nil
}

func hasLegacySchema(db *sql.DB) (bool, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return tables["series"] && !tables["bookmarks"], nil
}

// Close closes the database.
func (s *State) Close() error { return s.db.Close() }

// -- bookmarks ---------------------------------------------------------------

const bookmarkColumns = `
	b.slug, COALESCE(b.series_id, ''), b.url, COALESCE(b.title, ''),
	COALESCE(b.location, ''), b.enabled, b.added_at,
	COALESCE(b.last_run, ''), COALESCE(b.last_result, ''),
	(SELECT COUNT(*) FROM pages p WHERE p.slug = b.slug AND p.status = 'ok'),
	(SELECT COUNT(DISTINCT p.chapter) FROM pages p WHERE p.slug = b.slug AND p.status = 'ok')`

type scanner interface {
	Scan(dest ...any) error
}

func scanBookmark(r scanner) (*Bookmark, error) {
	b := &Bookmark{}
	var enabled int
	err := r.Scan(
		&b.Slug, &b.SeriesID, &b.URL, &b.Title, &b.Location, &enabled,
		&b.AddedAt, &b.LastRun, &b.LastResult, &b.Pages, &b.Chapters,
	)
	if err != nil {
		return nil, err
	}
	b.Enabled = enabled != 0
	return b, nil
}

// UpsertBookmark inserts a bookmark or re-enables an existing one. Returns true
// when the bookmark was newly created.
func (s *State) UpsertBookmark(slug, seriesID, rawURL, title string) (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM bookmarks WHERE slug = ?`, slug).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		_, err := s.db.Exec(
			`UPDATE bookmarks SET url = ?, series_id = ?, title = CASE WHEN ? <> '' THEN ? ELSE title END, enabled = 1 WHERE slug = ?`,
			rawURL, seriesID, title, title, slug,
		)
		return false, err
	}
	_, err := s.db.Exec(
		`INSERT INTO bookmarks(slug, series_id, url, title, enabled, added_at) VALUES(?,?,?,?,1,?)`,
		slug, seriesID, rawURL, title, Now(),
	)
	return true, err
}

// ListBookmarks returns all bookmarks with chapter/page counts.
func (s *State) ListBookmarks() ([]Bookmark, error) {
	return s.queryBookmarks(`SELECT ` + bookmarkColumns + ` FROM bookmarks b ORDER BY b.added_at, b.slug`)
}

// EnabledBookmarks returns only enabled bookmarks.
func (s *State) EnabledBookmarks() ([]Bookmark, error) {
	return s.queryBookmarks(`SELECT ` + bookmarkColumns + ` FROM bookmarks b WHERE b.enabled = 1 ORDER BY b.added_at, b.slug`)
}

func (s *State) queryBookmarks(query string) ([]Bookmark, error) {
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bookmark
	for rows.Next() {
		b, err := scanBookmark(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// GetBookmark returns one bookmark, or nil when missing.
func (s *State) GetBookmark(slug string) (*Bookmark, error) {
	row := s.db.QueryRow(`SELECT `+bookmarkColumns+` FROM bookmarks b WHERE b.slug = ?`, slug)
	b, err := scanBookmark(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// RemoveBookmark deletes a bookmark (downloaded files are kept).
func (s *State) RemoveBookmark(slug string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM bookmarks WHERE slug = ?`, slug)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetLocation points a bookmark at its filesystem folder.
func (s *State) SetLocation(slug, path string) error {
	_, err := s.db.Exec(`UPDATE bookmarks SET location = ? WHERE slug = ?`, path, slug)
	return err
}

// RecordBookmarkRun stores the last run time and result for a bookmark.
func (s *State) RecordBookmarkRun(slug, result string) error {
	_, err := s.db.Exec(`UPDATE bookmarks SET last_run = ?, last_result = ? WHERE slug = ?`, Now(), result, slug)
	return err
}

// -- chapters ----------------------------------------------------------------

// SetChapter upserts a discovered chapter.
func (s *State) SetChapter(slug, number, rawURL, status string, pageCount int) error {
	_, err := s.db.Exec(`
		INSERT INTO chapters(slug, number, url, status, page_count, updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(slug, number) DO UPDATE SET
			url = excluded.url,
			status = excluded.status,
			page_count = excluded.page_count,
			updated_at = excluded.updated_at`,
		slug, number, rawURL, status, pageCount, Now(),
	)
	return err
}

// EnsureChapter inserts a chapter row when none exists yet.
func (s *State) EnsureChapter(slug, number string, pageCount int) error {
	_, err := s.db.Exec(`
		INSERT INTO chapters(slug, number, url, status, page_count, updated_at)
		VALUES(?,?,'','ok',?,?)
		ON CONFLICT(slug, number) DO NOTHING`,
		slug, number, pageCount, Now(),
	)
	return err
}

// AllChaptersForBookmark returns all known chapter numbers (from the chapters
// table and from synced pages), sorted ascending.
func (s *State) AllChaptersForBookmark(slug string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT number FROM chapters WHERE slug = ? UNION SELECT chapter FROM pages WHERE slug = ?`,
		slug, slug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nums []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if n != "" {
			nums = append(nums, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	discover.SortChapters(nums)
	return nums, nil
}

// -- pages -------------------------------------------------------------------

// GetPage returns the page row for (slug, chapter, index), or nil.
func (s *State) GetPage(slug, chapter string, index int) (*Page, error) {
	row := s.db.QueryRow(`
		SELECT slug, chapter, page_index, url, COALESCE(path, ''),
		       COALESCE(etag, ''), COALESCE(last_modified, ''), COALESCE(sha256, ''),
		       COALESCE(size, 0), status, updated_at
		FROM pages WHERE slug = ? AND chapter = ? AND page_index = ?`,
		slug, chapter, index,
	)
	p := &Page{}
	err := row.Scan(
		&p.Slug, &p.Chapter, &p.Index, &p.URL, &p.Path, &p.ETag,
		&p.LastModified, &p.SHA256, &p.Size, &p.Status, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// UpsertPage records a successfully downloaded page.
func (s *State) UpsertPage(slug, chapter string, index int, rawURL, path, etag, lastModified, sha256 string, size int64) error {
	_, err := s.db.Exec(`
		INSERT INTO pages(slug, chapter, page_index, url, path, etag, last_modified, sha256, size, status, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,'ok',?)
		ON CONFLICT(slug, chapter, page_index) DO UPDATE SET
			url = excluded.url,
			path = excluded.path,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			sha256 = excluded.sha256,
			size = excluded.size,
			status = 'ok',
			updated_at = excluded.updated_at`,
		slug, chapter, index, rawURL, path, nullable(etag), nullable(lastModified), sha256, size, Now(),
	)
	return err
}

// IntegratePage inserts a page row found on disk when none exists for
// (slug, chapter, index). Returns true when a row was inserted.
func (s *State) IntegratePage(slug, chapter string, index int, path, sha256 string, size int64) (bool, error) {
	res, err := s.db.Exec(`
		INSERT INTO pages(slug, chapter, page_index, url, path, etag, last_modified, sha256, size, status, updated_at)
		VALUES(?,?,?,'',?,NULL,NULL,?,?,'ok',?)
		ON CONFLICT(slug, chapter, page_index) DO NOTHING`,
		slug, chapter, index, path, sha256, size, Now(),
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetPagePath updates the stored file path of an existing page row (used when
// a bookmark's folder moves and the pages are re-located on disk).
func (s *State) SetPagePath(slug, chapter string, index int, path string) error {
	_, err := s.db.Exec(
		`UPDATE pages SET path = ?, updated_at = ? WHERE slug = ? AND chapter = ? AND page_index = ?`,
		path, Now(), slug, chapter, index,
	)
	return err
}

// PagesForBookmark returns all synced page rows of a bookmark.
func (s *State) PagesForBookmark(slug string) ([]Page, error) {
	rows, err := s.db.Query(`
		SELECT slug, chapter, page_index, url, COALESCE(path, ''),
		       COALESCE(etag, ''), COALESCE(last_modified, ''), COALESCE(sha256, ''),
		       COALESCE(size, 0), status, updated_at
		FROM pages WHERE slug = ? AND status = 'ok'
		ORDER BY chapter, page_index`,
		slug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Page
	for rows.Next() {
		p := Page{}
		if err := rows.Scan(
			&p.Slug, &p.Chapter, &p.Index, &p.URL, &p.Path, &p.ETag,
			&p.LastModified, &p.SHA256, &p.Size, &p.Status, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountPagesForChapters counts synced pages of the given chapters.
func (s *State) CountPagesForChapters(slug string, chapters []string) (int64, error) {
	if len(chapters) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(chapters)+1)
	args = append(args, slug)
	for _, c := range chapters {
		args = append(args, c)
	}
	var n int64
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM pages WHERE slug = ? AND chapter IN (`+placeholders(len(chapters))+`)`,
		args...,
	).Scan(&n)
	return n, err
}

// UnsyncChapters deletes the page rows of the given chapters so the next run
// downloads them again. Returns the number of deleted rows.
func (s *State) UnsyncChapters(slug string, chapters []string) (int64, error) {
	if len(chapters) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(chapters)+1)
	args = append(args, slug)
	for _, c := range chapters {
		args = append(args, c)
	}
	res, err := s.db.Exec(
		`DELETE FROM pages WHERE slug = ? AND chapter IN (`+placeholders(len(chapters))+`)`,
		args...,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// -- runs --------------------------------------------------------------------

// RecordRun stores a sync run summary.
func (s *State) RecordRun(slug, startedAt, finishedAt string, newPages, skipped, failed, lockedChapters int, note string) error {
	_, err := s.db.Exec(`
		INSERT INTO runs(slug, started_at, finished_at, new_pages, skipped, failed, locked_chapters, note)
		VALUES(?,?,?,?,?,?,?,?)`,
		slug, startedAt, finishedAt, newPages, skipped, failed, lockedChapters, note,
	)
	return err
}

// -- helpers -----------------------------------------------------------------

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

// ParsePageIndex is a small helper for callers handling filenames.
func ParsePageIndex(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
