package commands

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestPrintUsage(t *testing.T) {
	fs := newFlagSet("run", "sync every registered bookmark")
	fs.Bool("yes", false, "answer yes")
	fs.Bool("y", false, "alias for --yes")
	fs.Bool("demo", false, "preview output without syncing: `MODE`")

	var buf bytes.Buffer
	printUsage(&buf, fs, "sync every registered bookmark")

	for _, want := range []string{
		"imgsync run — sync every registered bookmark",
		"Usage",
		"imgsync run [options]",
		"Options",
		"--demo[=MODE]",
		"--yes",
		"-y",
		"'imgsync <command> -h'",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("usage =\n%s\nwant %q", buf.String(), want)
		}
	}
}

func TestFlagExitCode(t *testing.T) {
	if got := flagExitCode(flag.ErrHelp); got != 0 {
		t.Errorf("flagExitCode(ErrHelp) = %d, want 0", got)
	}
	if got := flagExitCode(errors.New("bad flag")); got != 2 {
		t.Errorf("flagExitCode(error) = %d, want 2", got)
	}
}
