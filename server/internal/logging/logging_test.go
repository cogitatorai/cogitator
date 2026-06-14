package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewHandlerLevelFiltering(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		wantDebug bool
		wantWarn  bool
	}{
		{"debug", "debug", true, true},
		{"info", "info", false, true},
		{"empty defaults to info", "", false, true},
		{"bogus falls back to info", "bogus", false, true},
		{"warn", "warn", false, true},
		{"warning alias", "warning", false, true},
		{"uppercase DEBUG", "DEBUG", true, true},
		{"error", "error", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(NewHandler(&buf, tt.level, "text"))
			logger.Debug("dbg-line")
			logger.Warn("warn-line")
			gotDebug := strings.Contains(buf.String(), "dbg-line")
			gotWarn := strings.Contains(buf.String(), "warn-line")
			if gotDebug != tt.wantDebug {
				t.Errorf("level %q: debug emitted = %v, want %v", tt.level, gotDebug, tt.wantDebug)
			}
			if gotWarn != tt.wantWarn {
				t.Errorf("level %q: warn emitted = %v, want %v", tt.level, gotWarn, tt.wantWarn)
			}
		})
	}
}

func TestNewHandlerJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(&buf, "info", "json"))
	logger.Info("hello", "key", "value")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if entry["msg"] != "hello" || entry["key"] != "value" {
		t.Errorf("unexpected entry: %v", entry)
	}
}

func TestNewHandlerJSONFormatUppercase(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(&buf, "info", "JSON"))
	logger.Info("uppercase-json")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("uppercase JSON format: output is not JSON: %v\n%s", err, buf.String())
	}
	if entry["msg"] != "uppercase-json" {
		t.Errorf("unexpected msg: %v", entry["msg"])
	}
}

func TestNewHandlerTextDefault(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(&buf, "info", ""))
	logger.Info("hello")
	if !strings.Contains(buf.String(), "msg=hello") {
		t.Errorf("expected text format, got: %s", buf.String())
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input  string
		want   slog.Level
		wantOK bool
	}{
		{"", slog.LevelInfo, true},
		{"info", slog.LevelInfo, true},
		{"INFO", slog.LevelInfo, true},
		{"debug", slog.LevelDebug, true},
		{"DEBUG", slog.LevelDebug, true},
		{"warn", slog.LevelWarn, true},
		{"warning", slog.LevelWarn, true},
		{"WARNING", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"bogus", slog.LevelInfo, false},
		{"verbose", slog.LevelInfo, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := parseLevel(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseLevel(%q): ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("parseLevel(%q): level = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		wantJSON bool
		wantOK   bool
	}{
		{"", false, true},
		{"text", false, true},
		{"TEXT", false, true},
		{"json", true, true},
		{"JSON", true, true},
		{"yaml", false, false},
		{"logfmt", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotJSON, ok := parseFormat(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseFormat(%q): ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if gotJSON != tt.wantJSON {
				t.Errorf("parseFormat(%q): json = %v, want %v", tt.input, gotJSON, tt.wantJSON)
			}
		})
	}
}
