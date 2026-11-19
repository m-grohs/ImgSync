package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"imgsync/commands"
	"imgsync/config"
)

// Run dispatches the CLI. It returns the process exit code.
func Run(args []string) int {
	verbose := false
	for len(args) > 0 && (args[0] == "-v" || args[0] == "--verbose") {
		verbose = true
		args = args[1:]
	}
	setupLogging(verbose)

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
	case "unsync":
		return commands.Unsync(args[1:])
	case "run":
		return commands.Run(args[1:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command: %s\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func setupLogging(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	var w io.Writer = os.Stderr
	if f, err := os.OpenFile(config.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		w = io.MultiWriter(os.Stderr, f)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
}

func usage(w io.Writer) {
	fmt.Fprint(w, `imgsync - incremental image sync for comic series

Usage:
  imgsync add <url...> [-f FILE]              register bookmark(s)
  imgsync list                                show registered bookmarks
  imgsync remove <slug>                       stop syncing, keep downloaded files
  imgsync set-output <dir>                    set the folder new chapters download to
  imgsync set-location <slug> <path>          point a bookmark at its folder [--dry-run]
  imgsync unsync <slug> [chapter...]          mark chapter(s) unsynced [--dry-run]
  imgsync run [--series SLUG] [--dry-run] [--verify] [--limit N]

Global flags:
  -v, --verbose
`)
}
