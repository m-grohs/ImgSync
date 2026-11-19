package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const appName = "imgsync"

// Config is the on-disk configuration (~/.config/imgsync/config.toml).
type Config struct {
	Output string `toml:"output,omitempty"`
}

func configDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appName)
}

func dataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, appName)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func configPath() string { return filepath.Join(configDir(), "config.toml") }

// DBPath is the SQLite state database.
func DBPath() string { return filepath.Join(dataDir(), "state.sqlite") }

// LogPath is the log file.
func LogPath() string { return filepath.Join(dataDir(), "imgsync.log") }

// Load reads config.toml (empty config when missing).
func Load() Config {
	var cfg Config
	if data, err := os.ReadFile(configPath()); err == nil {
		_ = toml.Unmarshal(data, &cfg)
	}
	return cfg
}

// ExpandPath expands a leading ~ and makes relative paths absolute.
func ExpandPath(p string) string {
	if p == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// OutputDir is where chapters download when a bookmark has no location.
func OutputDir() string {
	raw := os.Getenv("IMGSYNC_OUTPUT")
	if raw == "" {
		raw = Load().Output
	}
	if raw == "" {
		raw = "~/Pictures/test"
	}
	dir := ExpandPath(raw)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// SetOutput writes the download folder to config.toml and returns its absolute
// path.
func SetOutput(dir string) (string, error) {
	abs := ExpandPath(dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	cfg := Load()
	cfg.Output = abs
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return "", err
	}
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(configPath(), []byte(buf.String()), 0o644); err != nil {
		return "", err
	}
	return abs, nil
}
