package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"imgsync/commands"
	"imgsync/internal/ui"
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
	case "export":
		return commands.Export(args[1:])
	case "import":
		return commands.Import(args[1:])
	case "list":
		return commands.List(args[1:])
	case "remove":
		return commands.Remove(args[1:])
	case "reset":
		return commands.Reset(args[1:])
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

type usageRow struct {
	command string
	args    string
	detail  string
}

func usage(w io.Writer) {
	st := ui.Style{Enabled: ui.ColorsEnabled(w)}
	rows := []usageRow{
		{"add", "<url...>", "register one or more series URLs"},
		{"export", "[-o FILE]", "export bookmarks as txt, csv or json"},
		{"import", "FILE", "import bookmarks from a file"},
		{"list", "", "show registered bookmarks"},
		{"remove", "<id|slug>", "stop syncing, keep downloaded files"},
		{"reset", "[--yes]", "delete all state and restart IDs at 1"},
		{"set-output", "<dir>", "set the folder new chapters download to"},
		{"set-location", "<id|slug> <path>", "point a bookmark at its folder"},
		{"assume-synced", "<id|slug> <chapter>", "trust chapters up to a cutoff as downloaded"},
		{"unsync", "<id|slug> [chapter...]", "mark chapters for re-download"},
		{"run", "[options]", "sync every registered bookmark"},
		{"version", "", "print build information"},
	}

	fmt.Fprintf(w, "%s %s %s\n", st.Name("imgsync"), ui.EmDash, "incremental image sync for comic series")
	fmt.Fprintln(w)
	fmt.Fprintln(w, st.Bold("Usage"))
	fmt.Fprintln(w, "  imgsync <command> [options]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, st.Bold("Commands"))
	width := 0
	for _, row := range rows {
		plain := row.command
		if row.args != "" {
			plain += " " + row.args
		}
		if n := utf8.RuneCountInString(plain); n > width {
			width = n
		}
	}
	for _, row := range rows {
		plain := row.command
		if row.args != "" {
			plain += " " + row.args
		}
		left := st.Name(row.command)
		if row.args != "" {
			left += " " + st.Dim(row.args)
		}
		fmt.Fprintf(w, "  %s%s  %s\n", left, strings.Repeat(" ", width-utf8.RuneCountInString(plain)), row.detail)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, st.Bold("Bookmarks"))
	fmt.Fprintf(w, "  %s %s\n", st.Dim(ui.Dot), "Use a list ID, unique slug prefix, or full slug from imgsync list.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, st.Bold("Output"))
	fmt.Fprintf(w, "  %s %s\n", st.Dim(ui.Dot), "Live progress bars appear on interactive terminals; detailed logs stay in the log file.")
	fmt.Fprintf(w, "  %s %s\n", st.Dim(ui.Dot), "Preview the final messages with imgsync run --demo.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, st.Bold("Global flags"))
	fmt.Fprintf(w, "  %s  %s\n", st.Name("-v, --verbose"), "mirror detailed logs to the terminal")
}
