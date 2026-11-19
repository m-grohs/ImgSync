package commands

import (
	"flag"
	"strings"
)

// parseArgs parses flags even when they appear after positional arguments
// (the stdlib flag package stops at the first non-flag argument). It returns
// the positional arguments.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		if !strings.HasPrefix(rest[0], "-") || rest[0] == "-" {
			positionals = append(positionals, rest[0])
			args = rest[1:]
			continue
		}
		args = rest
	}
}
