package commands

import (
	"flag"
	"fmt"
	"os"

	"imgsync/config"
)

// SetOutput stores the download folder in config.toml.
func SetOutput(args []string) int {
	fs := flag.NewFlagSet("set-output", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(os.Stderr, "usage: imgsync set-output <dir>")
		return 2
	}

	path, err := config.SetOutput(pos[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("output set to: %s\n", path)
	return 0
}
