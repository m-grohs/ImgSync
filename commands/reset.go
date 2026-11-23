package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"imgsync/config"
	"imgsync/state"
)

// Reset deletes the state database so the next bookmark gets ID 1 again.
// Downloaded files and config are kept.
func Reset(args []string) int {
	return reset(args, os.Stdin, stdinIsInteractive(os.Stdin))
}

func reset(args []string, in io.Reader, interactive bool) int {
	fs := newFlagSetWithUsage("reset", "delete all state and start bookmark IDs from 1", "[--yes]")
	yes := fs.Bool("yes", false, "delete the state database without asking")
	fs.BoolVar(yes, "y", false, "alias for --yes")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExitCode(err)
	}
	if len(pos) != 0 {
		fmt.Fprintln(os.Stderr, "usage: imgsync reset [--yes]")
		return 2
	}

	path := config.DBPath()
	if !*yes {
		if !interactive {
			fmt.Fprintln(os.Stderr, "error: reset needs confirmation; rerun with --yes")
			return 1
		}
		fmt.Printf("This deletes %s and all bookmarks, synced-page and run history. Downloaded files and config are kept. Continue? [y/N] ", path)
		answer, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("reset aborted")
			return 1
		}
	}

	if err := state.Reset(path); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("state reset: %s\n", path)
	fmt.Println("bookmark IDs start at 1 again; downloaded files and config were kept")
	return 0
}

func stdinIsInteractive(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
