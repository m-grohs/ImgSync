package state

// schema statements are executed one by one on open.
var schema = []string{
	`PRAGMA journal_mode=WAL;`,

	`CREATE TABLE IF NOT EXISTS bookmarks (
		slug        TEXT PRIMARY KEY,
		series_id   TEXT,
		url         TEXT NOT NULL,
		title       TEXT,
		location    TEXT,
		assume_synced_through TEXT NOT NULL DEFAULT '',
		enabled     INTEGER NOT NULL DEFAULT 1,
		added_at    TEXT NOT NULL,
		last_run    TEXT,
		last_result TEXT
	);`,

	`CREATE TABLE IF NOT EXISTS bookmark_ids (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT NOT NULL UNIQUE
	);`,

	`CREATE TABLE IF NOT EXISTS assume_exceptions (
		slug    TEXT NOT NULL,
		chapter TEXT NOT NULL,
		PRIMARY KEY (slug, chapter)
	);`,

	`CREATE TABLE IF NOT EXISTS chapters (
		slug        TEXT NOT NULL,
		number      TEXT NOT NULL,
		url         TEXT NOT NULL,
		status      TEXT NOT NULL,
		page_count  INTEGER,
		updated_at  TEXT NOT NULL,
		PRIMARY KEY (slug, number)
	);`,

	`CREATE TABLE IF NOT EXISTS pages (
		slug          TEXT NOT NULL,
		chapter       TEXT NOT NULL,
		page_index    INTEGER NOT NULL,
		url           TEXT NOT NULL DEFAULT '',
		path          TEXT,
		etag          TEXT,
		last_modified TEXT,
		sha256        TEXT,
		size          INTEGER,
		status        TEXT NOT NULL DEFAULT 'ok',
		updated_at    TEXT NOT NULL,
		PRIMARY KEY (slug, chapter, page_index)
	);`,

	`CREATE TABLE IF NOT EXISTS runs (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		slug            TEXT NOT NULL,
		started_at      TEXT NOT NULL,
		finished_at     TEXT NOT NULL,
		new_pages       INTEGER NOT NULL DEFAULT 0,
		skipped         INTEGER NOT NULL DEFAULT 0,
		failed          INTEGER NOT NULL DEFAULT 0,
		locked_chapters INTEGER NOT NULL DEFAULT 0,
		note            TEXT
	);`,
}
