package commands

import (
	"flag"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"imgsync/internal/version"
)

func TestParseArgsFlagsAfterPositionals(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&discardWriter{})
	n := fs.Int("n", 0, "")
	got, err := parseArgs(fs, []string{"a", "--n", "7", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) || *n != 7 {
		t.Fatalf("positionals = %v, n = %d", got, *n)
	}
}

func TestParseArgsTerminator(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&discardWriter{})
	got, err := parseArgs(fs, []string{"a", "--", "-not-a-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a", "-not-a-flag"}) {
		t.Fatalf("positionals = %v", got)
	}
}

func TestVersion(t *testing.T) {
	if code := Version(nil); code != 0 {
		t.Fatalf("Version() = %d", code)
	}
	if code := Version([]string{"extra"}); code != 2 {
		t.Fatalf("Version(extra) = %d", code)
	}
}

func TestHelpExitCodes(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = devNull
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = devNull.Close()
	})

	commands := []struct {
		name string
		run  func([]string) int
	}{
		{"add", Add},
		{"assume-synced", AssumeSynced},
		{"export", Export},
		{"import", Import},
		{"list", List},
		{"remove", Remove},
		{"reset", Reset},
		{"run", Run},
		{"set-location", SetLocation},
		{"set-output", SetOutput},
		{"unsync", Unsync},
		{"version", Version},
	}
	for _, cmd := range commands {
		if got := cmd.run([]string{"-h"}); got != 0 {
			t.Errorf("%s -h = %d, want 0", cmd.name, got)
		}
	}
}

func TestVersionStringUsesInjectedVersion(t *testing.T) {
	old := version.Version
	version.Version = "v9.8.7"
	t.Cleanup(func() { version.Version = old })

	got := VersionString()
	if !strings.HasPrefix(got, "imgsync v9.8.7 ") {
		t.Fatalf("VersionString() = %q, want injected version", got)
	}
	if !strings.Contains(got, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("VersionString() = %q, want platform", got)
	}
}

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }
