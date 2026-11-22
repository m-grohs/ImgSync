package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"imgsync/internal/ui"
)

// newFlagSet builds a flag set with the shared help style used by every
// subcommand.
func newFlagSet(name, synopsis string) *flag.FlagSet {
	return newFlagSetWithUsage(name, synopsis, "")
}

// newFlagSetWithUsage is like newFlagSet, but replaces the generic "[options]"
// usage suffix for commands that take positional arguments.
func newFlagSetWithUsage(name, synopsis, args string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printUsage(fs.Output(), fs, synopsis, args) }
	return fs
}

// flagExitCode maps the standard -h result to success; other parse errors stay
// usage errors.
func flagExitCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

type helpRow struct {
	label string
	usage string
}

func printUsage(w io.Writer, fs *flag.FlagSet, synopsis string, args ...string) {
	usageArgs := "[options]"
	if len(args) > 0 && args[0] != "" {
		usageArgs = args[0]
	}
	st := ui.Style{Enabled: ui.ColorsEnabled(w)}
	rows := make([]helpRow, 0, 12)
	fs.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		dashes := "--"
		if len(f.Name) == 1 {
			dashes = "-"
		}
		label := dashes + f.Name
		switch {
		case name == "":
		case isBoolFlag(f):
			label += "[=" + name + "]"
		default:
			label += " " + name
		}
		rows = append(rows, helpRow{label: label, usage: usage})
	})

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s %s\n", st.Name("imgsync "+fs.Name()), ui.EmDash, synopsis)
	fmt.Fprintln(w)
	fmt.Fprintln(w, st.Bold("Usage"))
	fmt.Fprintf(w, "  imgsync %s %s\n", fs.Name(), usageArgs)
	if len(rows) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, st.Bold("Options"))
		width := 0
		for _, row := range rows {
			if n := utf8.RuneCountInString(row.label); n > width {
				width = n
			}
		}
		for _, row := range rows {
			fmt.Fprintf(w, "  %s%s  %s\n",
				st.Name(row.label), strings.Repeat(" ", width-utf8.RuneCountInString(row.label)), row.usage)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s %s %s %s\n",
		st.Dim(ui.Dot), "'imgsync <command> -h'", ui.EmDash, st.Dim("show command options"))
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}
