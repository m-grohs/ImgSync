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
	for name, got := range map[string]string{
		"success": s.Success("done"),
		"error":   s.Error("failed"),
		"warning": s.Warning("wait"),
		"bold":    s.Bold("head"),
		"dim":     s.Dim("meta"),
		"name":    s.Name("title"),
	} {
		if !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, "\x1b[0m") {
			t.Errorf("%s = %q, want ANSI wrapper", name, got)
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
