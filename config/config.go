package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	appName             = "imgsync"
	defaultOutputFolder = "~/Downloads/Images"
)

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
	return filepath.Join(base, appName)
}

// EnsureDataDir creates the state/log directory on demand.
func EnsureDataDir() error { return os.MkdirAll(dataDir(), 0o755) }

func configPath() string { return filepath.Join(configDir(), "config.toml") }

// DBPath is the SQLite state database.
func DBPath() string { return filepath.Join(dataDir(), "state.sqlite") }

// LogPath is the log file.
func LogPath() string { return filepath.Join(dataDir(), "imgsync.log") }

// Load reads config.toml. Malformed configuration is an error so a broken
// file never silently redirects downloads to a different folder.
func Load() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", configPath(), err)
	}
	return cfg, nil
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

// OutputDir is where chapters download when a bookmark has no location. It
// does not create the directory; downloads create folders as they are written.
func OutputDir() (string, error) {
	raw := os.Getenv("IMGSYNC_OUTPUT")
	if raw == "" {
		cfg, err := Load()
		if err != nil {
			return "", err
		}
		raw = cfg.Output
	}
	if raw == "" {
		raw = defaultOutputFolder
	}
	return ExpandPath(raw), nil
}

// SetOutput writes the download folder to config.toml and returns its absolute
// path. The config file is replaced atomically.
func SetOutput(dir string) (string, error) {
	abs := ExpandPath(dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	cfg, err := Load()
	if err != nil {
		return "", err
	}
	cfg.Output = abs
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return "", err
	}
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return "", err
	}
	return abs, writeFileAtomic(configPath(), []byte(buf.String()), 0o644)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	fh, err := os.CreateTemp(dir, ".imgsync-config-*")
	if err != nil {
		return err
	}
	tmp := fh.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := fh.Chmod(perm); err != nil {
		_ = fh.Close()
		return err
	}
	if _, err := fh.Write(data); err != nil {
		_ = fh.Close()
		return err
	}
	if err := fh.Sync(); err != nil {
		_ = fh.Close()
		return err
	}
	if err := fh.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
