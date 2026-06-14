// Package logging configures the process-wide slog default logger.
// Level and format come from config/env (COGITATOR_LOG_LEVEL,
// COGITATOR_LOG_FORMAT); JSON output makes Docker/Fly log aggregation usable.
// Unrecognized non-empty values are silently defaulted by NewHandler and
// warned about by Setup (the warning is emitted through the newly installed
// logger so it appears in the configured format).
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// parseLevel converts a level string to slog.Level.
// Returns (LevelInfo, true) for "" or "info", the matching level for known
// values, and (LevelInfo, false) for unrecognized non-empty strings.
func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, true
	case "debug":
		return slog.LevelDebug, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// parseFormat converts a format string to a json bool.
// Returns (false, true) for "" or "text", (true, true) for "json", and
// (false, false) for unrecognized non-empty strings.
func parseFormat(s string) (json bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text":
		return false, true
	case "json":
		return true, true
	}
	return false, false
}

// Setup installs the default slog logger writing to stderr.
// level: debug|info|warn|warning|error (default info).
// format: text|json (default text).
// Invalid values fall back to defaults so a bad env var cannot break startup;
// a warning is logged through the new handler for each unrecognized value.
func Setup(level, format string) {
	slog.SetDefault(slog.New(NewHandler(os.Stderr, level, format)))
	if _, ok := parseLevel(level); !ok {
		slog.Warn("unrecognized log level, defaulting to info", "value", level)
	}
	if _, ok := parseFormat(format); !ok {
		slog.Warn("unrecognized log format, defaulting to text", "value", format)
	}
}

// NewHandler builds a slog handler for the given writer, level, and format.
// Split from Setup so tests can capture output.
// Unrecognized level/format values are silently defaulted; use Setup to get
// a warning logged.
func NewHandler(w io.Writer, level, format string) slog.Handler {
	lvl, _ := parseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}
	useJSON, _ := parseFormat(format)
	if useJSON {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}
