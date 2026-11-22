// Package logging configures the process-wide slog logger and allows
// commands to raise the level after flag parsing.
package logging

import (
	"io"
	"log/slog"
	"os"
	"sync/atomic"

	"imgsync/config"
	"imgsync/progress"
)

const maxLogBytes = 5 << 20 // 5 MiB per log file, one rotated backup

var (
	level   = new(slog.LevelVar)
	logFile *os.File
	console atomic.Bool
)

// Setup configures slog. Records always go to the log file; they are mirrored
// to stderr only when verbose output is requested.
func Setup(verbose bool) {
	if verbose {
		level.Set(slog.LevelDebug)
	} else {
		level.Set(slog.LevelInfo)
	}
	console.Store(verbose)

	if err := config.EnsureDataDir(); err == nil {
		path := config.LogPath()
		if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
			_ = os.Rename(path, path+".1")
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			logFile = f
		}
	}
	rebuild()
}

// SetVerbose raises the log level to debug and mirrors records to stderr. It
// never lowers the level set at startup, so a global -v keeps working.
func SetVerbose(verbose bool) {
	if !verbose {
		return
	}
	level.Set(slog.LevelDebug)
	console.Store(true)
	rebuild()
}

func rebuild() {
	var w io.Writer
	if logFile != nil {
		w = logFile
	}
	switch {
	case w == nil && console.Load():
		w = progress.Writer(os.Stderr)
	case w == nil:
		// Never let log records leak into the quiet terminal output when the
		// log file could not be opened.
		w = io.Discard
	case console.Load():
		w = io.MultiWriter(w, progress.Writer(os.Stderr))
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
}

// Close releases the log file, if one was opened.
func Close() error {
	if logFile == nil {
		return nil
	}
	err := logFile.Close()
	logFile = nil
	return err
}
