package commands

import (
	"bytes"
	"errors"
	"flag"
	"os"
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

func TestCommandHelpUsageLines(t *testing.T) {
	tests := []struct {
		name string
		run  func([]string) int
		want string
	}{
		{"add", Add, "  imgsync add <url...>\n"},
		{"export", Export, "  imgsync export [-o FILE]\n"},
		{"import", Import, "  imgsync import FILE\n"},
		{"list", List, "  imgsync list [<id|slug>]\n"},
		{"remove", Remove, "  imgsync remove <id|slug>\n"},
		{"reset", Reset, "  imgsync reset [--yes]\n"},
		{"set-output", SetOutput, "  imgsync set-output <dir>\n"},
		{"set-location", SetLocation, "  imgsync set-location <id|slug> <path>\n"},
		{"assume-synced", AssumeSynced, "  imgsync assume-synced <id|slug> <chapter> | <id|slug> --clear\n"},
		{"unsync", Unsync, "  imgsync unsync <id|slug> [chapter|range...]\n"},
		{"run", Run, "  imgsync run [options]\n"},
		{"version", Version, "  imgsync version\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := captureHelpStderr(t, tt.run)
			if code != 0 {
				t.Fatalf("%s -h = %d, want 0", tt.name, code)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("help =\n%s\nwant usage line %q", out, tt.want)
			}
		})
	}
}

func captureHelpStderr(t *testing.T, fn func([]string) int) (int, string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	defer func() {
		os.Stderr = old
		_ = f.Close()
	}()

	code := fn([]string{"-h"})
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(data)
}

func TestFlagExitCode(t *testing.T) {
	if got := flagExitCode(flag.ErrHelp); got != 0 {
		t.Errorf("flagExitCode(ErrHelp) = %d, want 0", got)
	}
	if got := flagExitCode(errors.New("bad flag")); got != 2 {
		t.Errorf("flagExitCode(error) = %d, want 2", got)
	}
}
