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
imgsync list
imgsync set-output ~/Pictures/ImgSync        # folder new chapters download to
imgsync set-location 3 ~/Pictures/ImgSync    # point a bookmark at its existing folder
imgsync set-location blade <path>            # unique slug prefix works too
imgsync set-location <slug> <path> --dry-run # report only
imgsync set-location <id|slug> <path> --assume-synced-through 123
imgsync assume-synced 3 123                  # trust chapters up to 123 as downloaded
imgsync assume-synced 3 --clear              # remove the assumption
imgsync unsync 3                             # mark all chapters for re-download
imgsync unsync 3 "chapter 003" --dry-run
imgsync remove 3                             # stop syncing, keep downloaded files

imgsync run
imgsync run --series 3                       # only one bookmark
imgsync run --dry-run                        # show what would be downloaded
imgsync run --verify                         # conditional GET for db-synced pages
imgsync run --limit 10                       # max 10 new pages per bookmark
imgsync run --no-progress                    # hide the progress bar
imgsync run --verbose                        # show per-chapter indexing details
imgsync version                              # build information
```

`--limit` applies separately to every bookmark. With `--verify`, all chapter
pages are still indexed so changed pages can be found, but at most N new images
are downloaded per bookmark. Unchanged images (304 or identical bytes) count as
unchanged, not new.

Wherever a command takes a bookmark, you can use the `ID` shown by
`imgsync list` (e.g. `3`), a unique slug prefix (e.g. `blade`), or the full
slug. Ambiguous prefixes are rejected with the matching slugs.

## Progress

On interactive terminals `imgsync run` shows a live progress bar on stderr,
first while indexing chapter pages and then while downloading. Both show the
bookmark and the current chapter:

```
myst-might-mayhem  Chapter 116  [████████░░░░░░░░░░░░░░░░]  94%  116/123 chapters  ETA 0:04
myst-might-mayhem  Chapter 116  [███░░░░░░░░░░░░░░░░░░░░░]  12/25 pages  3.1/s  ETA 0:04
```

The ETA is based on the work known after indexing, so already-synced pages
(which are skipped without a request) do not distort it. The bar is disabled
automatically when stderr is redirected, when `TERM=dumb`, or when
`IMGSYNC_NO_PROGRESS=1` is set; `--no-progress` turns it off per run.

Per-chapter indexing details are only logged with `imgsync run --verbose` (or
the global `-v` flag).

## Run summary

Each bookmark reports the chapters it downloaded, followed by a total line:

```
msg=synced bookmark=myst-might-mayhem-7ba78ec7 note="+4 chapters (Chapter 122-125), 71 pages, 121 chapters assumed"
done: +4 chapters (71 pages), 291 pages unchanged, 121 chapters assumed
```

"assumed" counts chapters trusted through `assume-synced`; "up to date" counts
chapters whose pages were already completely in the database.

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

## Database is the single source of truth

A page is considered synced when `state.sqlite` has a row for it, or when its
chapter is at or below the bookmark's assumed cutoff. A chapter is considered
synced once all of its pages have rows. Otherwise the tool does not care
whether the file still exists or where it was moved.

- Moved or deleted files are **not** re-downloaded.
- Fully synced chapters are skipped during indexing, so later runs do not even
  fetch their chapter page. Use `imgsync run --verify` to re-index everything
  and re-check pages with conditional GETs.
- `imgsync unsync <slug> [chapter...]` deletes the synced rows for one or all
  chapters of a bookmark and marks them for indexing again; the next
  `imgsync run` downloads them again (into the bookmark's current
  location/output folder).
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
pages or images. Newer chapters sync normally. Individual chapters can be
forced back into the download queue with `imgsync unsync <slug> <chapter>`;
`imgsync unsync <slug>` clears the cutoff entirely. Assumed chapters are not
verified, so missing pages inside them stay missing.

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
- Chapters whose pages are all in the DB are marked synced and skipped during
  indexing; their chapter page is not fetched again.
- Pages the DB marks as synced are skipped without a request (unless
  `--verify`, which sends a conditional GET).
- Chapters reported as premium/locked are recorded and retried on the next run.
- Downloads go to `*.part`, are hashed (SHA-256) and renamed atomically.
- Requests use browser headers and HTTP/2, 3 concurrent connections, 300 ms
  spacing, retry/backoff on 429/5xx, and honor `Retry-After`.
