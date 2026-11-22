package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imgsync/config"
)

func TestSetupKeepsTerminalQuietUnlessVerbose(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	console, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = console
	oldDefault := slog.Default()
	t.Cleanup(func() {
		os.Stderr = oldStderr
		slog.SetDefault(oldDefault)
		_ = Close()
		_ = console.Close()
	})

	Setup(false)
	slog.Info("quiet-record")
	quiet, err := os.ReadFile(console.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(quiet), "quiet-record") {
		t.Errorf("non-verbose setup wrote to terminal: %q", quiet)
	}

	Setup(true)
	slog.Info("loud-record")
	loud, err := os.ReadFile(console.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(loud), "loud-record") {
		t.Errorf("verbose setup did not mirror to terminal: %q", loud)
	}

	logged, err := os.ReadFile(config.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"quiet-record", "loud-record"} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("log file missing %q: %q", want, logged)
		}
	}
}
