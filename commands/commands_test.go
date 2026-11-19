package commands

import (
	"flag"
	"reflect"
	"testing"
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

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }
