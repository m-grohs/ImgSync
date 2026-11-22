package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{-time.Second, "0s"},
		{500 * time.Millisecond, "1s"},
		{42 * time.Second, "42s"},
		{64 * time.Second, "1m 04s"},
		{time.Hour, "1h 00m"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1h 02m"},
	}
	for _, tc := range cases {
		if got := Duration(tc.d); got != tc.want {
			t.Errorf("Duration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestStyleDisabledForNonTerminal(t *testing.T) {
	var buf bytes.Buffer
	s := Style{Enabled: ColorsEnabled(&buf)}
	if s.Enabled {
		t.Fatal("buffer must not enable colors")
	}
	if got := s.Success("done"); got != "done" {
		t.Errorf("Success() = %q, want plain text", got)
	}
}

func TestStyleEnabled(t *testing.T) {
	s := Style{Enabled: true}
	for _, tc := range []struct{ name, got, want string }{
		{"success", s.Success("done"), "\x1b[1;32mdone\x1b[0m"},
		{"error", s.Error("failed"), "\x1b[1;31mfailed\x1b[0m"},
		{"warning", s.Warning("wait"), "\x1b[1;33mwait\x1b[0m"},
		{"bold", s.Bold("head"), "\x1b[1mhead\x1b[0m"},
		{"dim", s.Dim("meta"), "\x1b[2mmeta\x1b[0m"},
		{"bar fill", s.BarFill("█"), "\x1b[32m█\x1b[0m"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
		if strings.Contains(tc.got, ";36m") {
			t.Errorf("%s = %q, teal must not be used", tc.name, tc.got)
		}
	}
	if got := s.Success(""); got != "" {
		t.Errorf("Success(empty) = %q", got)
	}
}

func TestColorsEnabledNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if ColorsEnabled(&bytes.Buffer{}) {
		t.Error("NO_COLOR should disable colors")
	}
}
