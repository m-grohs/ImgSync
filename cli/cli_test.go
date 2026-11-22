package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsage(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)
	for _, want := range []string{
		"imgsync — incremental image sync for comic series",
		"Usage",
		"Commands",
		"imgsync run",
		"--demo",
		"Global flags",
		"-v, --verbose",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("usage =\n%s\nwant %q", buf.String(), want)
		}
	}
}

func TestRunExitCodes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cases := []struct {
		args []string
		want int
	}{
		{nil, 2},
		{[]string{"--help"}, 0},
		{[]string{"help"}, 0},
		{[]string{"version"}, 0},
		{[]string{"-v", "version"}, 0},
		{[]string{"nope"}, 2},
	}
	for _, tc := range cases {
		if got := Run(tc.args); got != tc.want {
			t.Errorf("Run(%v) = %d, want %d", tc.args, got, tc.want)
		}
	}
}
