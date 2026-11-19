package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("IMGSYNC_OUTPUT", "")
	return home
}

func TestOutputDirDefault(t *testing.T) {
	home := isolatedConfig(t)
	got, err := OutputDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Downloads", "Images")
	if got != want {
		t.Fatalf("OutputDir() = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("OutputDir must not create the folder, stat err = %v", err)
	}
}

func TestLoadRejectsMalformedTOML(t *testing.T) {
	isolatedConfig(t)
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(), []byte("output = "), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected malformed config to be an error")
	}
	if _, err := OutputDir(); err == nil {
		t.Fatal("expected OutputDir to propagate the config error")
	}
}

func TestSetOutputWritesValidConfig(t *testing.T) {
	home := isolatedConfig(t)
	dir := filepath.Join(home, "pics")
	got, err := SetOutput(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("SetOutput() = %q, want %q", got, dir)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != dir {
		t.Fatalf("config output = %q, want %q", cfg.Output, dir)
	}
	data, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "output") {
		t.Fatalf("config = %q", data)
	}
	info, err := os.Stat(configPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("config mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestExpandPath(t *testing.T) {
	home := isolatedConfig(t)
	if got := ExpandPath("~/x"); got != filepath.Join(home, "x") {
		t.Fatalf("ExpandPath = %q", got)
	}
	if got := ExpandPath("~"); got != home {
		t.Fatalf("ExpandPath(~) = %q", got)
	}
	if !filepath.IsAbs(ExpandPath("rel")) {
		t.Fatal("relative path was not made absolute")
	}
}
