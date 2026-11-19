package commands

import (
	"flag"
	"slices"
	"strings"
)

// parseArgs parses flags even when they appear after positional arguments
// (the stdlib flag package stops at the first non-flag argument). An explicit
// "--" terminates flag parsing and keeps all following values positional.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	if i := slices.Index(args, "--"); i >= 0 {
		before, err := parseArgsNoTerminator(fs, args[:i])
		if err != nil {
			return nil, err
		}
		return append(before, args[i+1:]...), nil
	}
	return parseArgsNoTerminator(fs, args)
}

func parseArgsNoTerminator(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for len(args) > 0 {
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
	return positionals, nil
}
