package cli

import (
	"fmt"
	"io"
	"os"

	"imgsync/commands"
	"imgsync/logging"
)

// Run dispatches the CLI. It returns the process exit code.
func Run(args []string) int {
	verbose := false
	for len(args) > 0 && (args[0] == "-v" || args[0] == "--verbose") {
		verbose = true
		args = args[1:]
	}
	logging.Setup(verbose)

	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	switch args[0] {
	case "add":
		return commands.Add(args[1:])
	case "list":
		return commands.List(args[1:])
	case "remove":
		return commands.Remove(args[1:])
	case "set-output":
		return commands.SetOutput(args[1:])
	case "set-location":
		return commands.SetLocation(args[1:])
	case "assume-synced":
		return commands.AssumeSynced(args[1:])
	case "unsync":
		return commands.Unsync(args[1:])
	case "run":
		return commands.Run(args[1:])
	case "version":
		return commands.Version(args[1:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command: %s\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `imgsync - incremental image sync for comic series

Usage:
  imgsync add <url...> [-f FILE]              register bookmark(s)
  imgsync list                                show registered bookmarks
  imgsync remove <id|slug>                    stop syncing, keep downloaded files
  imgsync set-output <dir>                    set the folder new chapters download to
  imgsync set-location <id|slug> <path>       point a bookmark at its folder [--dry-run]
                                             [--assume-synced-through N]
  imgsync assume-synced <id|slug> <chapter>   trust chapters up to N as downloaded
  imgsync assume-synced <id|slug> --clear     remove the assumption
  imgsync unsync <id|slug> [chapter...]       mark chapter(s) unsynced [--dry-run]
  imgsync run [--series ID|SLUG] [--dry-run] [--verify] [--limit N] [--no-progress] [--verbose]
  imgsync version                             print build information

A bookmark can be referenced by its list ID (e.g. 3), a unique slug prefix,
or the full slug. "imgsync list" shows the IDs and slugs.

The run command shows a progress bar with an ETA on interactive terminals.
Set IMGSYNC_NO_PROGRESS=1 or pass --no-progress to hide it.

Global flags:
  -v, --verbose
`)
}
