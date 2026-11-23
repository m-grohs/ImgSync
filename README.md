# ImgSync

Incremental image sync for comic series. It downloads new chapters, leaves
existing files alone, and remembers what it synced in a SQLite database. Works
with Asura-style sites and generic chapter readers.

## Install

Prebuilt binaries for Windows, macOS, and Linux (amd64/arm64) are on the
[releases page](https://github.com/m-grohs/ImgSync/releases/latest). Download
the one for your platform, rename it to `imgsync` (`imgsync.exe` on Windows),
and put it in a folder on your PATH.

Windows PowerShell:

```powershell
$dir = Join-Path $env:USERPROFILE 'bin'
New-Item -ItemType Directory -Force $dir | Out-Null
Move-Item "$env:USERPROFILE\Downloads\imgsync-<version>-windows-amd64.exe" "$dir\imgsync.exe"
$env:Path += ";$dir"
```

To keep the PATH change, add `%USERPROFILE%\bin` to your user PATH in System
Properties.

macOS/Linux:

```sh
mkdir -p ~/.local/bin
mv ~/Downloads/imgsync-<version>-<platform> ~/.local/bin/imgsync
chmod +x ~/.local/bin/imgsync
export PATH="$HOME/.local/bin:$PATH"   # add this line to ~/.zshrc or ~/.bashrc
```

Check the install with `imgsync version`.

To build from source (Go 1.26+):

```sh
git clone https://github.com/m-grohs/ImgSync.git
cd ImgSync
go install .
```

The binary goes to `$(go env GOPATH)/bin` (usually `~/go/bin`); add that folder
to PATH if needed. `go install imgsync@latest` does not work because this is a
local module.

## Quick start

```sh
imgsync add https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7
imgsync set-output ~/Pictures/ImgSync
imgsync run
imgsync list
```

`add -f urls.txt` reads one URL per line. Lines starting with `#` are comments.

## Commands

Use the ID from `imgsync list`, a unique slug prefix, or the full slug wherever
`<ref>` appears.

| Command | Description |
| --- | --- |
| `add <url...>` | Register one or more series. `-f FILE` reads URLs from a file. |
| `list [ref]` | Show bookmarks, or every detail for one. |
| `run [options]` | Sync all bookmarks. See [Run options](#run-options). |
| `set-output <dir>` | Set the default download folder. |
| `set-location <ref> <dir>` | Point a bookmark at an existing folder and record the pages already there. |
| `assume-synced <ref> <chapter>` | Treat chapters up to a cutoff as downloaded. `--clear` removes the cutoff. |
| `unsync <ref> [chapter|range...]` | Re-download selected chapters; with no chapters, all of them. |
| `remove <ref>` | Stop syncing a series. Downloaded files stay. |
| `export [-o FILE]` | Export bookmarks as txt, csv, or json. |
| `import FILE` | Import bookmarks; `-` reads stdin. |
| `reset` | Delete all state and restart bookmark IDs at 1. Asks first. |
| `version` | Print build information. |

Every command has `-h` / `--help`.

### Chapters to re-download

`unsync` takes chapters and inclusive ranges, for example
`imgsync unsync 2 100-120 140-145 166`. The next `run` downloads them again.
`--dry-run` shows what would change.

`assume-synced` (or `set-location <ref> <dir> --assume-synced-through 123`) marks
older chapters as downloaded without checking them. Missing files in that range
stay missing. `imgsync unsync <ref>` clears the cutoff.

### Existing series folders

If a series folder already has files that are not in the database, `run` asks
before using it because files there may be overwritten. `--yes` accepts;
`--dry-run` only reports. Bookmarks with a location set by `set-location` are
exempt, since their files are already recorded.

### Import and export

`export` writes to stdout unless `-o FILE` is given. The format defaults to txt
and is inferred from the filename for `.csv` and `.json`. Use
`--format txt|csv|json` to override it. `import` uses the same rule and merges by
default: it adds new bookmarks and leaves existing ones alone. `--update` applies
fields from the file to existing bookmarks, `--dry-run` previews the changes, and
`--online` fetches titles for new ones. The file is validated before anything is
written.

Minimal txt format (one bookmark per line, `#` starts a comment):

```txt
# imgsync bookmarks v1
https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7 assume=127 except=104,110-115 title="Blade & Soul" location="~/Pictures/ImgSync/Blade & Soul"
```

csv and json are also supported; run `imgsync export -o bookmarks.csv` (or
`.json`) to see them. A missing field leaves an existing value alone, and `none`
clears it. Chapter ranges in `except` are expanded.

### Files and state

New chapters go to the bookmark's location, otherwise the global output folder
(default `~/Downloads/Images`; change it with `set-output` or `IMGSYNC_OUTPUT`).
The layout is `<folder>/<series>/<chapter label>/001.webp`.

State is in `~/.local/share/imgsync/state.sqlite`, logs in
`~/.local/share/imgsync/imgsync.log`, and config in
`~/.config/imgsync/config.toml`.

A page counts as synced when the database has a row for it or its chapter is at
or below the bookmark's assumed cutoff. Moved or deleted files are not
re-downloaded. Fully synced chapters are skipped, so `run --verify` is the way to
recheck them.

`reset` deletes the database, losing all bookmarks and sync history. It keeps
downloaded files, config, and logs. Scripts need `--yes` to skip the prompt. An
old Python `state.sqlite` at the same path is left alone and causes an error;
move it aside first.

## Run options

- `--series <ref>` - sync only that bookmark; repeat for several.
- `--dry-run` - show what would be downloaded.
- `--verify` - re-index chapters and recheck saved pages with conditional requests.
- `--limit N` - stop after N new pages per bookmark.
- `--yes` - use an existing series folder without asking.
