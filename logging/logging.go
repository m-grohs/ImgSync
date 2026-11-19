// Package logging configures the process-wide slog logger and allows
// commands to raise the level after flag parsing.
package logging

import (
	"io"
	"log/slog"
	"os"

	"imgsync/config"
	"imgsync/progress"
)

var level = new(slog.LevelVar)

// Setup configures slog to write to stderr and the log file.
func Setup(verbose bool) {
	if verbose {
		level.Set(slog.LevelDebug)
	} else {
		level.Set(slog.LevelInfo)
	}
	var w io.Writer = progress.Writer(os.Stderr)
	if f, err := os.OpenFile(config.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		w = io.MultiWriter(progress.Writer(os.Stderr), f)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
}

// SetVerbose raises the log level to debug. It never lowers the level set at
// startup, so a global -v keeps working.
func SetVerbose(verbose bool) {
	if verbose {
		level.Set(slog.LevelDebug)
	}
}
