# ImgSync

Standalone CLI for incremental image sync of comic series. Adds only what is
new, never deletes, and keeps the database as the single source of truth for
what has been synced.

## Install

Build from this directory:

```sh
go build -o imgsync .
```

Optionally put the binary on your PATH:

```sh
ln -s "$PWD/imgsync" ~/.local/bin/imgsync
```

## Usage

```sh
imgsync add https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7
imgsync add -f urls.txt                      # one URL per line, # comments allowed
imgsync list
imgsync set-output ~/Pictures/ImgSync        # folder new chapters download to
imgsync set-location <slug> <path>           # point a bookmark at its existing folder
imgsync set-location <slug> <path> --dry-run # report only
imgsync unsync <slug>                        # mark all chapters for re-download
imgsync unsync <slug> "chapter 003" --dry-run
imgsync remove <slug>                        # stop syncing, keep downloaded files

imgsync run
imgsync run --series <slug>                  # only one bookmark
imgsync run --dry-run                        # show what would be downloaded
imgsync run --verify                         # conditional GET for db-synced pages
imgsync run --limit 10                       # max 10 new pages per bookmark
```

## Where files go

New chapters download to the bookmark's `location` when set, otherwise to the
global output folder (default `~/Pictures/test`, override with
`imgsync set-output <dir>` or `IMGSYNC_OUTPUT`).

Layout:

```
<location or output>/<series folder>/<chapter label>/001.webp
```

Example:

```
~/Pictures/ImgSync/blade of the discarded/chapter 001/001.webp
```

Series folders and chapter labels are derived from the URL slug: the trailing
site-id hash is stripped and dashes become spaces
(`blade-of-the-discarded-7ba78ec7` → `blade of the discarded`, chapters
`chapter 001`, `chapter 12.5`).

## Database is the single source of truth

A page is considered synced when `state.sqlite` has a row for it. The tool does
not care whether the file still exists or where it was moved.

- Moved or deleted files are **not** re-downloaded.
- `imgsync unsync <slug> [chapter...]` deletes the synced rows for one or all
  chapters of a bookmark; the next `imgsync run` downloads them again (into the
  bookmark's current location/output folder).
- `imgsync set-location <slug> <path>` points a bookmark at a folder that
  already contains downloaded chapters. It scans `chapter NNN` folders, records
  the pages it finds in the DB, and reports:

  - chapters and pages found on disk
  - pages already in the DB (their stored paths are relinked to the new folder)
  - pages integrated into the DB (or "would integrate" with `--dry-run`)
  - pages the DB says are synced but are missing on disk

State lives in `~/.local/share/imgsync/state.sqlite`; logs in
`~/.local/share/imgsync/imgsync.log`; config in
`~/.config/imgsync/config.toml` (`output = "~/Pictures/ImgSync"`).

If a legacy Python `state.sqlite` (it has a `series` table) is still at that
path, ImgSync refuses to touch it and exits with an error. Move it away to let
ImgSync create a fresh database; the old file is never modified automatically.

## How it works

- The series page is parsed for chapter links (generic `/chapter/<n>` paths,
  with a link-text fallback for other sites).
- Chapter pages embed their page lists; on Asura sites the Astro
  `ChapterReader` props are used, otherwise images are collected in document
  order (`<img src>`, `srcset`, image links).
- Pages the DB marks as synced are skipped without a request (unless
  `--verify`, which sends a conditional GET).
- Chapters reported as premium/locked are recorded and retried on the next run.
- Downloads go to `*.part`, are hashed (SHA-256) and renamed atomically.
- Requests use browser headers and HTTP/2, 3 concurrent connections, 300 ms
  spacing, retry/backoff on 429/5xx, and honor `Retry-After`.

## Notes

Downloaded content is copyrighted. Keep the job rate-limited, use it for
personal archival only, and stop if the site asks you to.
