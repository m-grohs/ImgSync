package state

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"imgsync/discover"

	_ "modernc.org/sqlite"
)

// Chapter status values stored in the chapters table.
const (
	ChapterStatusPending = "pending"
	ChapterStatusOK      = "ok"
	ChapterStatusSynced  = "synced"
	ChapterStatusLocked  = "locked"
)

// State wraps the SQLite state database.
type State struct {
	db *sql.DB
}

// Bookmark is a registered series ("bookmark").
type Bookmark struct {
	ID                  int64
	Slug                string
	SeriesID            string
	URL                 string
	Title               string
	Location            string
	AssumeSyncedThrough string
	Enabled             bool
	AddedAt             string
	LastRun             string
	LastResult          string
	Chapters            int
	Pages               int
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
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
	if err := ensureColumn(db, "bookmarks", "assume_synced_through", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureBookmarkIDs(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// ensureBookmarkIDs assigns a stable ID to every bookmark that does not have
// one yet (for example bookmarks created before IDs were introduced).
func ensureBookmarkIDs(db *sql.DB) error {
	_, err := db.Exec(`
		INSERT INTO bookmark_ids(slug)
		SELECT b.slug FROM bookmarks b
		WHERE NOT EXISTS (SELECT 1 FROM bookmark_ids i WHERE i.slug = b.slug)
		ORDER BY b.added_at, b.slug`)
	return err
}

// ensureColumn adds a column to an existing table when it is missing. It is
// used for backwards-compatible schema upgrades.
func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notnull    int
			dflt       sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	// The pool has a single connection, so close the rows before executing.
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
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

// tx runs fn in a single transaction so multi-statement updates cannot leave
// the database in a half-updated state.
func (s *State) tx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// -- bookmarks ---------------------------------------------------------------

const bookmarkColumns = `
	COALESCE((SELECT i.id FROM bookmark_ids i WHERE i.slug = b.slug), 0),
	b.slug, COALESCE(b.series_id, ''), b.url, COALESCE(b.title, ''),
	COALESCE(b.location, ''), COALESCE(b.assume_synced_through, ''), b.enabled, b.added_at,
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
		&b.ID, &b.Slug, &b.SeriesID, &b.URL, &b.Title, &b.Location, &b.AssumeSyncedThrough,
		&enabled, &b.AddedAt, &b.LastRun, &b.LastResult, &b.Pages, &b.Chapters,
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
		if err != nil {
			return false, err
		}
		return false, s.ensureBookmarkID(slug)
	}
	_, err := s.db.Exec(
		`INSERT INTO bookmarks(slug, series_id, url, title, enabled, added_at) VALUES(?,?,?,?,1,?)`,
		slug, seriesID, rawURL, title, Now(),
	)
	if err != nil {
		return false, err
	}
	return true, s.ensureBookmarkID(slug)
}

// ensureBookmarkID reserves a stable ID for a bookmark without changing an
// existing one.
func (s *State) ensureBookmarkID(slug string) error {
	_, err := s.db.Exec(
		`INSERT INTO bookmark_ids(slug) VALUES(?) ON CONFLICT(slug) DO NOTHING`,
		slug,
	)
	return err
}

// BookmarkID returns the stable list ID of a bookmark.
func (s *State) BookmarkID(slug string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM bookmark_ids WHERE slug = ?`, slug).Scan(&id)
	return id, err
}

// ImportBookmark is one bookmark from an import file. Pointer fields are nil
// when the field was not present in the file, which means "leave it alone" for
// an existing bookmark. A *string value of "none" clears that field.
type ImportBookmark struct {
	Slug     string
	URL      string
	SeriesID string
	Assume   *string
	Except   *[]string
	Title    *string
	Location *string
	Enabled  *bool
}

// ImportResult describes what happened to one imported bookmark.
type ImportResult struct {
	Slug    string
	Created bool
	Updated bool
	Kept    bool
}

// ApplyImport creates missing bookmarks and, when update is true, applies the
// supplied fields to existing ones. Everything is written in one transaction
// so a failure cannot leave a half-imported database.
func (s *State) ApplyImport(entries []ImportBookmark, update bool) ([]ImportResult, error) {
	results := make([]ImportResult, 0, len(entries))
	err := s.tx(func(tx *sql.Tx) error {
		for _, e := range entries {
			var count int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM bookmarks WHERE slug = ?`, e.Slug).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				if err := s.insertImportedBookmark(tx, e); err != nil {
					return err
				}
				results = append(results, ImportResult{Slug: e.Slug, Created: true})
				continue
			}
			if !update {
				results = append(results, ImportResult{Slug: e.Slug, Kept: true})
				continue
			}
			if err := s.updateImportedBookmark(tx, e); err != nil {
				return err
			}
			results = append(results, ImportResult{Slug: e.Slug, Updated: true})
		}
		return nil
	})
	return results, err
}

func (s *State) insertImportedBookmark(tx *sql.Tx, e ImportBookmark) error {
	assume, title, location := "", "", ""
	if e.Assume != nil && *e.Assume != "none" {
		assume = *e.Assume
	}
	if e.Title != nil && *e.Title != "none" {
		title = *e.Title
	}
	if e.Location != nil && *e.Location != "none" {
		location = *e.Location
	}
	enabled := 1
	if e.Enabled != nil && !*e.Enabled {
		enabled = 0
	}
	if _, err := tx.Exec(
		`INSERT INTO bookmarks(slug, series_id, url, title, location, assume_synced_through, enabled, added_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		e.Slug, e.SeriesID, e.URL, title, location, assume, enabled, Now(),
	); err != nil {
		return err
	}
	if err := ensureBookmarkIDTx(tx, e.Slug); err != nil {
		return err
	}
	return replaceAssumeExceptions(tx, e.Slug, e.Except)
}

func (s *State) updateImportedBookmark(tx *sql.Tx, e ImportBookmark) error {
	sets := []string{"url = ?", "series_id = ?"}
	args := []any{e.URL, e.SeriesID}
	if e.Assume != nil {
		value := *e.Assume
		if value == "none" {
			value = ""
		}
		sets = append(sets, "assume_synced_through = ?")
		args = append(args, value)
	}
	if e.Title != nil {
		value := *e.Title
		if value == "none" {
			value = ""
		}
		sets = append(sets, "title = ?")
		args = append(args, value)
	}
	if e.Location != nil {
		value := *e.Location
		if value == "none" {
			value = ""
		}
		sets = append(sets, "location = ?")
		args = append(args, value)
	}
	if e.Enabled != nil {
		value := 1
		if !*e.Enabled {
			value = 0
		}
		sets = append(sets, "enabled = ?")
		args = append(args, value)
	}
	args = append(args, e.Slug)
	if _, err := tx.Exec(`UPDATE bookmarks SET `+strings.Join(sets, ", ")+` WHERE slug = ?`, args...); err != nil {
		return err
	}
	if e.Assume != nil && *e.Assume == "none" {
		if _, err := tx.Exec(`DELETE FROM assume_exceptions WHERE slug = ?`, e.Slug); err != nil {
			return err
		}
	}
	return replaceAssumeExceptions(tx, e.Slug, e.Except)
}

func replaceAssumeExceptions(tx *sql.Tx, slug string, chapters *[]string) error {
	if chapters == nil {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM assume_exceptions WHERE slug = ?`, slug); err != nil {
		return err
	}
	for _, number := range *chapters {
		if _, err := tx.Exec(
			`INSERT INTO assume_exceptions(slug, chapter) VALUES(?, ?) ON CONFLICT(slug, chapter) DO NOTHING`,
			slug, number,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureBookmarkIDTx(tx *sql.Tx, slug string) error {
	_, err := tx.Exec(`INSERT INTO bookmark_ids(slug) VALUES(?) ON CONFLICT(slug) DO NOTHING`, slug)
	return err
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ResolveBookmark looks a bookmark up by exact slug, numeric list ID or a
// unique slug prefix (in that order). It returns nil when nothing matches and
// an error listing the candidates when a prefix is ambiguous.
func (s *State) ResolveBookmark(ref string) (*Bookmark, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}

	if b, err := s.GetBookmark(ref); err != nil || b != nil {
		return b, err
	}

	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		row := s.db.QueryRow(
			`SELECT `+bookmarkColumns+` FROM bookmarks b WHERE b.slug = (SELECT slug FROM bookmark_ids WHERE id = ?)`,
			id,
		)
		b, err := scanBookmark(row)
		if err == nil {
			return b, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}

	rows, err := s.db.Query(
		`SELECT `+bookmarkColumns+` FROM bookmarks b
		 WHERE substr(b.slug, 1, length(?)) = ?
		 ORDER BY b.slug LIMIT 5`,
		ref, ref,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []Bookmark
	for rows.Next() {
		b, err := scanBookmark(rows)
		if err != nil {
			return nil, err
		}
		matches = append(matches, *b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Slug
		}
		return nil, fmt.Errorf("ambiguous bookmark %q: matches %s", ref, strings.Join(names, ", "))
	}
}

// RemoveBookmark deletes a bookmark (downloaded files and their page rows are
// kept so re-adding the same series does not re-download). Run history and
// bookkeeping rows are removed atomically with the bookmark.
func (s *State) RemoveBookmark(slug string) (bool, error) {
	var removed bool
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM bookmarks WHERE slug = ?`, slug)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		removed = n > 0
		if !removed {
			return nil
		}
		for _, stmt := range []string{
			`DELETE FROM assume_exceptions WHERE slug = ?`,
			`DELETE FROM bookmark_ids WHERE slug = ?`,
			`DELETE FROM runs WHERE slug = ?`,
		} {
			if _, err := tx.Exec(stmt, slug); err != nil {
				return err
			}
		}
		return nil
	})
	return removed, err
}

// SetLocation points a bookmark at its filesystem folder.
func (s *State) SetLocation(slug, path string) error {
	_, err := s.db.Exec(`UPDATE bookmarks SET location = ? WHERE slug = ?`, path, slug)
	return err
}

// SetAssumeSyncedThrough treats every chapter up to and including number as
// already downloaded without storing page rows for it.
func (s *State) SetAssumeSyncedThrough(slug, number string) error {
	_, err := s.db.Exec(`UPDATE bookmarks SET assume_synced_through = ? WHERE slug = ?`, number, slug)
	return err
}

// ClearAssumeSynced removes the chapter cutoff and all of its exceptions.
func (s *State) ClearAssumeSynced(slug string) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM assume_exceptions WHERE slug = ?`, slug); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE bookmarks SET assume_synced_through = '' WHERE slug = ?`, slug)
		return err
	})
}

// AssumeExceptions returns the chapters that override the bookmark's cutoff.
func (s *State) AssumeExceptions(slug string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT chapter FROM assume_exceptions WHERE slug = ?`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var chapter string
		if err := rows.Scan(&chapter); err != nil {
			return nil, err
		}
		out[chapter] = true
	}
	return out, rows.Err()
}

// AddAssumeException forces a chapter below the cutoff to be downloaded.
func (s *State) AddAssumeException(slug, chapter string) error {
	return s.AddAssumeExceptions(slug, []string{chapter})
}

// AddAssumeExceptions forces several chapters below the cutoff to be
// downloaded. Existing exceptions are kept, and all inserts are atomic.
func (s *State) AddAssumeExceptions(slug string, chapters []string) error {
	if len(chapters) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(
			`INSERT INTO assume_exceptions(slug, chapter) VALUES(?, ?) ON CONFLICT(slug, chapter) DO NOTHING`,
		)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, chapter := range chapters {
			if _, err := stmt.Exec(slug, chapter); err != nil {
				return err
			}
		}
		return nil
	})
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

// SyncedChapters returns the chapter numbers that are fully downloaded, so a
// run can skip fetching and indexing them.
func (s *State) SyncedChapters(slug string) (map[string]bool, error) {
	rows, err := s.db.Query(
		`SELECT number FROM chapters WHERE slug = ? AND status = ?`,
		slug, ChapterStatusSynced,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			return nil, err
		}
		out[number] = true
	}
	return out, rows.Err()
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
	if errors.Is(err, sql.ErrNoRows) {
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
	// Force the next run to index these chapters again instead of skipping
	// them as fully synced. Both statements must succeed together: deleting
	// rows while leaving status "synced" would permanently hide the chapter.
	var deleted int64
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`DELETE FROM pages WHERE slug = ? AND chapter IN (`+placeholders(len(chapters))+`)`,
			args...,
		)
		if err != nil {
			return err
		}
		deleted, err = res.RowsAffected()
		if err != nil {
			return err
		}
		updateArgs := make([]any, 0, len(args)+1)
		updateArgs = append(updateArgs, ChapterStatusPending)
		updateArgs = append(updateArgs, args...)
		_, err = tx.Exec(
			`UPDATE chapters SET status = ? WHERE slug = ? AND number IN (`+placeholders(len(chapters))+`)`,
			updateArgs...,
		)
		return err
	})
	return deleted, err
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
