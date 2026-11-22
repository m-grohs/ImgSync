package commands

import (
	"bytes"
	"strings"
	"testing"

	"imgsync/progress"
)

func TestDemoModeSet(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "success"},
		{"true", "success"},
		{"success", "success"},
		{"empty", "empty"},
		{"ERROR", "error"},
		{"interrupt", "interrupt"},
	}
	for _, tc := range cases {
		var mode demoMode
		if err := mode.Set(tc.input); err != nil {
			t.Errorf("Set(%q) = %v", tc.input, err)
			continue
		}
		if string(mode) != tc.want {
			t.Errorf("Set(%q) = %q, want %q", tc.input, mode, tc.want)
		}
	}
	var mode demoMode
	if err := mode.Set("nope"); err == nil {
		t.Error("Set(nope) should fail")
	}
}

func TestRunDemoModes(t *testing.T) {
	was := progress.Enabled()
	progress.SetEnabled(false)
	t.Cleanup(func() { progress.SetEnabled(was) })

	cases := []struct {
		mode string
		want string
	}{
		{"success", "Sync complete"},
		{"empty", "All caught up"},
		{"error", "Sync finished with 2 problems"},
		{"interrupt", "Interrupted after"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		runDemo(&buf, tc.mode)
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("runDemo(%q) = %q, want %q", tc.mode, buf.String(), tc.want)
		}
		if strings.Contains(buf.String(), "time=") || strings.Contains(buf.String(), "level=") {
			t.Errorf("runDemo(%q) leaked log noise: %q", tc.mode, buf.String())
		}
	}
}
