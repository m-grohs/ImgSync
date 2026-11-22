# ImgSync

Standalone CLI for incremental image sync of comic series. Adds only what is
new, never deletes, and keeps the database as the single source of truth for
what has been synced.

## Install

### Download a release (recommended)

Prebuilt binaries are published on the
[GitHub Releases page](https://github.com/m-grohs/ImgSync/releases/latest).
Each release contains:

| Asset | Platform |
| --- | --- |
| `imgsync-<version>-windows-amd64.exe` | Windows x86-64 (Intel/AMD) |
| `imgsync-<version>-windows-arm64.exe` | Windows ARM64 (Snapdragon) |
| `imgsync-<version>-linux-amd64` | Linux x86-64 |
| `imgsync-<version>-linux-arm64` | Linux ARM64 |
| `imgsync-<version>-darwin-amd64` | macOS Intel |
| `imgsync-<version>-darwin-arm64` | macOS Apple Silicon |
| `SHA256SUMS-<version>.txt` | Checksums for all binaries |

On Windows, download the `.exe` for your CPU, then in PowerShell (replace the
version in the filename with the one you downloaded):

```powershell
$binDir = Join-Path $env:USERPROFILE 'bin'
New-Item -ItemType Directory -Force $binDir | Out-Null
Move-Item "$env:USERPROFILE\Downloads\imgsync-v0.1.0-windows-amd64.exe" "$binDir\imgsync.exe"

# Add the folder to your user PATH once, then use it in this shell too.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$binDir*") {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$binDir", 'User')
}
$env:Path += ";$binDir"
imgsync version
```

Newly opened terminals pick up the PATH change automatically. You can also do
the same via the GUI: **Settings → System → About → Advanced system settings →
Environment variables → User variables → Path → New**, and add
`%USERPROFILE%\bin`. Do not copy the exe into `C:\Windows\System32`.

On macOS/Linux, download the binary for your platform, then:

```sh
mkdir -p ~/.local/bin
mv ~/Downloads/imgsync-v0.1.0-linux-amd64 ~/.local/bin/imgsync   # use your downloaded file
chmod +x ~/.local/bin/imgsync

# Only needed if ~/.local/bin is not already on your PATH (use ~/.bashrc on Linux Bash):
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

Open a new terminal (or run `source ~/.zshrc`) and run `imgsync version`. The
output includes the release version, commit, and platform, for example
`imgsync v0.1.0 (a1b2c3d) windows/amd64`.

### Install with Go (from a checkout)

Requires Go 1.26 or newer. Run `go install` from the repository root:

```sh
git clone https://github.com/m-grohs/ImgSync.git
cd ImgSync
go install .
```

`go install .` places the binary in `$(go env GOPATH)/bin` — normally
`~/go/bin` (Windows: `%USERPROFILE%\go\bin`) — **not** in the current
directory. Add that folder to your PATH:

```powershell
# Windows PowerShell
$goBin = Join-Path (go env GOPATH) 'bin'
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$goBin*") {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$goBin", 'User')
}
$env:Path += ";$goBin"
imgsync version
```

```sh
# macOS/Linux (use ~/.bashrc if your shell is Bash)
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
```

Because this module's import path is just `imgsync`, `go install imgsync@latest`
does not work; install from a checkout as above, or use a release binary.

### Build the exe yourself

From the repository root:

```powershell
# Windows PowerShell — produces dist\imgsync.exe
go build -o dist/imgsync.exe .
.\dist\imgsync.exe version
```

```sh
# macOS/Linux — produces dist/imgsync
go build -o dist/imgsync .
./dist/imgsync version
```

To run the binary from any directory, copy it into a folder that is already on
your PATH (for example `%USERPROFILE%\bin` on Windows or `~/.local/bin` on
macOS/Linux), and add that folder to PATH using the commands above. A locally
built binary reports `imgsync dev (...)`; release binaries report their
`vX.Y.Z` tag.

## Usage

```sh
imgsync add https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7
imgsync add -f urls.txt                      # one URL per line, # comments allowed
imgsync export [-o bookmarks.txt]            # export bookmarks + assumptions (txt, csv, json)
imgsync export -o bookmarks.csv              # format inferred from the file extension
imgsync import bookmarks.txt [--dry-run] [--update] [--online]
imgsync list
imgsync list 3                               # show all details for one bookmark
imgsync set-output ~/Pictures/ImgSync        # folder new chapters download to
imgsync set-location 3 ~/Pictures/ImgSync    # point a bookmark at its existing folder
imgsync set-location blade <path>            # unique slug prefix works too
imgsync set-location <slug> <path> --dry-run # report only
imgsync set-location <id|slug> <path> --assume-synced-through 123
imgsync assume-synced 3 123                  # trust chapters up to 123 as downloaded
imgsync assume-synced 3 --clear              # remove the assumption
imgsync unsync 3                             # mark all chapters for re-download
imgsync unsync 3 "chapter 003" --dry-run
imgsync unsync 2 100-120 140-145 166        # several chapters/ranges at once
imgsync remove 3                             # stop syncing, keep downloaded files
imgsync reset                                # delete all state, IDs start at 1 (asks first)
imgsync reset --yes                          # same, no prompt (for scripts)

imgsync run
imgsync run --series 3                       # only one bookmark
imgsync run --dry-run                        # show what would be downloaded
imgsync run --verify                         # conditional GET for db-synced pages
imgsync run --yes                            # accept existing-folder prompts
imgsync run --limit 10                       # max 10 new pages per bookmark
imgsync run --no-progress                    # hide the progress bar
imgsync run --verbose                        # mirror detailed logs to the terminal
imgsync run --demo                           # preview the success output, no syncing
imgsync run --demo=error                     # also: empty, interrupt
imgsync version                              # build information
```

Every command has a styled `-h` / `--help` page that exits successfully.
`imgsync -h` lists all commands; `imgsync run -h` lists the run flags.

`--limit` applies separately to every bookmark. With `--verify`, all chapter
pages are still indexed so changed pages can be found, but at most N new images
are downloaded per bookmark. Unchanged images (304 or identical bytes) count as
unchanged, not new.

Before downloading, `run` checks whether a series folder already exists without
any pages or chapters of that bookmark recorded in the DB. It asks once before
using such a folder, since files at the same destination path may be overwritten.
Answering no skips only that bookmark; a scripted run without `--yes` fails instead of
guessing. `--dry-run` reports the conflict without prompting. Bookmarks with an
explicit location are exempt, because `set-location` already records the files
found there in the DB.

Wherever a command takes a bookmark, you can use the `ID` shown by
`imgsync list` (e.g. `3`), a unique slug prefix (e.g. `blade`), or the full
slug. Ambiguous prefixes are rejected with the matching slugs.

## Listing bookmarks

Plain `imgsync list` stays deliberately short: ID, display name, and the
assumed-through cutoff. The display name is the site title, falling back to a
readable slug-derived name (site hash removed, dashes replaced with spaces)
when no title was stored.

```
imgsync — registered bookmarks

  #1  The Beginning After the End             assumed through Chapter 344
  #2  The Divine Demon's Grand Ascension      —

2 bookmarks · 'imgsync list <id>' for details
```

Pass a bookmark reference to see everything else for that one bookmark:

```
Bookmark #2 — The Divine Demon's Grand Ascension

  Slug        the-divine-demons-grand-ascension
  URL         https://asurascans.com/comics/the-divine-demons-grand-ascension-...
  Series ID   ...
  Status      ✓ enabled
  Assumed     Chapter 074
  Exceptions  Chapter 104, 110-115
  Chapters    12
  Pages       291
  Location    ~/Pictures/ImgSync/The Divine Demon's Grand Ascension
  Added       2026-11-22T10:04:11Z
  Last run    2026-11-22T11:22:09Z
  Last result ✓ ok
```

`imgsync list <id|slug>` accepts the same ID, unique slug prefix, or full slug
that the other commands accept.

## Progress

On interactive terminals `imgsync run` shows one live bar at a time on stderr.
Indexing first counts series, then the chapters actually requested; downloads
then use a single bar per series and change the detail column as chapters
change. Finished bars are cleared, so no per-chapter history accumulates.

```
Indexing · Blade & Soul  12/137 chapters  [████░░░░░░░░░░░░░░░░░]   8%  2.4/s  ~52s
The Beginning After the End · Chapter 338  18/58 pages  [██████████░░░░░░░░░░░░]  31%  4.0/s  ~10s
```

The series name is the bookmark title, falling back to a readable slug-derived
name. Chapters skipped by `assume-synced` or because they are already up to date are not
counted, and `--limit` reduces the chapter total accordingly. Bars are disabled
when stderr is redirected, when `TERM=dumb`, or when `IMGSYNC_NO_PROGRESS=1` is
set; `--no-progress` turns them off per run.

Detailed `slog` records always go to the log file. With `imgsync run --verbose`
(or the global `-v` flag) they are also mirrored to the terminal above the bar;
without it the terminal stays quiet until the final summary.

## Run summary

After the bars are cleared, one styled block reports the outcome. On success:

```
✓ Sync complete in 1m 04s — 4 chapters, 71 pages downloaded

  The Beginning After the End · Chapter 338-339, 343-344
  The Divine Demon's Grand Ascension · Chapter 074

  291 pages unchanged · 807 chapters assumed · 3 bookmarks
```

When pages fail, the headline changes and only affected series are listed:

```
✗ Sync finished with 2 problems in 1m 04s

  Red Storm
    3 pages failed
  The Beginning After the End
    HTTP 403 (blocked)
```

An interrupted run saves the pages already downloaded:

```
⚠ Interrupted after 42s — progress saved, rerun to continue

  1 chapter, 18 pages downloaded
```

"assumed" counts chapters trusted through `assume-synced`; "up to date" counts
chapters whose pages were already completely in the database. Exit codes are
`0` for success, `1` when problems were reported, and `130` for an interrupt.

`imgsync run --demo` previews these messages without touching the database, the
network, or downloaded files. It accepts `success` (the default), `empty`,
`error`, and `interrupt`, and always exits `0`. On a non-interactive terminal
only the final messages are shown because progress bars are suppressed there.

## Where files go

New chapters download to the bookmark's `location` when set, otherwise to the
global output folder (default `~/Downloads/Images`, override with
`imgsync set-output <dir>` or `IMGSYNC_OUTPUT`).

Layout:

```
<location or output>/<series folder>/<chapter label>/001.webp
```

Example:

```
~/Pictures/ImgSync/The Genius Knight of a Fallen Race/Chapter 001/001.webp
```

Series folders use the series title as shown on the site, keeping its
capitalization. When no title is known, the slug is used with the trailing
site-id hash stripped and dashes converted to spaces
(`blade-of-the-discarded-7ba78ec7` → `blade of the discarded`). Chapter labels
are always derived from the chapter number (`Chapter 001`, `Chapter 12.5`).

Folders created by older versions from the lowercase slug or with lowercase
`chapter NNN` labels are renamed to the canonical names on the next run, and
their stored page paths are updated automatically.

## Import and export bookmarks

`export` writes every bookmark together with its assumed-synced cutoff,
assumption exceptions, title, location and enabled state. It prints to stdout
unless `-o FILE` is given. The format defaults to `txt`; with `-o`, it is
inferred from the extension (`.csv`, `.json`, otherwise txt). Override it with
`--format txt|csv|json`.

`import` reads the same three formats, inferring from the file extension
(`-` reads stdin). By default it **merges**: new bookmarks are added, existing
ones are left alone. Pass `--update` to apply the fields present in the file to
existing bookmarks, `--dry-run` to print the plan without writing, and
`--online` to fetch titles for new bookmarks (otherwise titles come from the
file or fall back to the slug). The whole file is validated before anything is
written, so an invalid entry aborts the import.

A field that is **absent** means "leave the existing value alone"; `none` means
"clear it". Chapter ranges in `except` are expanded (`110-115` → six chapters).

**txt** (default, one bookmark per line; `#` starts a comment):

```txt
# imgsync bookmarks v1
https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7 assume=127 except=104,110-115 title="Blade & Soul" location="~/Pictures/ImgSync/Blade & Soul"
https://asurascans.com/comics/foo-abc123 assume=12.5
```

**csv** (header row, empty cell = absent):

```csv
url,assume,except,title,location,enabled
https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7,127,"104,110-115",Blade & Soul,~/Pictures/ImgSync/Blade & Soul,1
```

**json** (missing key = absent, `[]` clears `except`):

```json
{
  "format": "imgsync-bookmarks",
  "version": 1,
  "bookmarks": [
    {
      "url": "https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7",
      "assume": "127",
      "except": ["104", "110-115"],
      "title": "Blade & Soul",
      "location": "~/Pictures/ImgSync/Blade & Soul",
      "enabled": true
    }
  ]
}
```

If a file lists a `location` that does not exist on this machine (or points to
a file instead of a directory), the import warns and ignores that field. New
bookmarks then use the global output folder, and an existing bookmark keeps its
current location. Convert an old `# N` assumption file by moving the number to
the preceding URL line:

```txt
# before
https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7
# 127

# after
https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7 assume=127
```

Imported assumptions are still trusted, not verified: chapters at or below the
cutoff are skipped without checking the files on disk.

## Database is the single source of truth

A page is considered synced when `state.sqlite` has a row for it, or when its
chapter is at or below the bookmark's assumed cutoff. A chapter is considered
synced once all of its pages have rows. Otherwise the tool does not care
whether the file still exists or where it was moved.

- Moved or deleted files are **not** re-downloaded.
- Fully synced chapters are skipped during indexing, so later runs do not even
  fetch their chapter page. Use `imgsync run --verify` to re-index everything
  and re-check pages with conditional GETs.
- `imgsync unsync <slug> [chapter|range...]` deletes the synced rows for the
  listed chapters (or all chapters when none are given) and marks them for
  indexing again; the next `imgsync run` downloads them again (into the
  bookmark's current location/output folder). Ranges are inclusive integer
  chapters, and several chapters and ranges can be combined in one command:
  `imgsync unsync 2 100-120 140-145 166`. Invalid or reversed ranges are
  rejected instead of being ignored.
- `imgsync set-location <id|slug> <path>` points a bookmark at a folder that
  already contains downloaded chapters. It scans `Chapter NNN` folders, records
  the pages it finds in the DB, and reports:

  - chapters and pages found on disk
  - pages already in the DB (their stored paths are relinked to the new folder)
  - pages integrated into the DB (or "would integrate" with `--dry-run`)
  - pages the DB says are synced but are missing on disk

### Trusting an existing range

If a folder was downloaded by another tool and its page filenames do not match
ImgSync's `001.webp` layout, tell ImgSync to trust everything up to a chapter
instead of re-downloading it:

```sh
imgsync set-location <slug> <path> --assume-synced-through 123
# or later:
imgsync assume-synced <slug> 123
imgsync assume-synced <slug> --clear
```

Chapters at or below the cutoff are skipped without fetching their chapter
pages or images. Newer chapters sync normally. Individual chapters or ranges
of chapters can be removed from the assumption with
`imgsync unsync <slug> 104-200 250`; they will re-download on the next run.
`imgsync unsync <slug>` clears the cutoff entirely. Assumed chapters are not
verified, so missing pages inside them stay missing.

State lives in `~/.local/share/imgsync/state.sqlite`; logs in
`~/.local/share/imgsync/imgsync.log`; config in
`~/.config/imgsync/config.toml` (`output = "~/Pictures/ImgSync"`).

`imgsync reset` deletes `state.sqlite` (and its SQLite sidecar files) and
creates an empty database, so the next bookmark gets ID 1 again. All bookmarks,
synced-page rows and run history are lost. Downloaded chapters, `config.toml`
and the log file are kept. The command asks for confirmation on a terminal;
scripts must pass `--yes` (or `-y`). Since the new DB has no record of synced
pages, the next run asks before using an existing series folder (or use
`imgsync run --yes`).

If a legacy Python `state.sqlite` (it has a `series` table) is still at that
path, ImgSync refuses to touch it and exits with an error. Move it away to let
ImgSync create a fresh database; the old file is never modified automatically.

## How it works

- The series page is parsed for chapter links (generic `/chapter/<n>` paths,
  with a link-text fallback for other sites).
- Chapter pages embed their page lists; on Asura sites the Astro
  `ChapterReader` props are used, otherwise images are collected in document
  order (`<img src>`, `srcset`, image links). Reader containers and chapter
  image markers are preferred so site logos and footer art are not mistaken
  for pages.
- Chapters whose pages are all in the DB are marked synced and skipped during
  indexing; their chapter page is not fetched again.
- Pages the DB marks as synced are skipped without a request (unless
  `--verify`, which sends a conditional GET).
- Chapters reported as premium/locked are recorded and retried on the next run.
- Downloads go to `*.part`, are hashed (SHA-256) and renamed atomically.
- Requests use browser headers and HTTP/2, 3 concurrent connections, 300 ms
  spacing, retry/backoff on 429/5xx, and honor `Retry-After`.
