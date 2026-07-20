package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNew_TTY_UsesTextHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo, true)

	logger.Info("hello", "key", "value")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") {
		t.Errorf("expected text-handler output containing 'msg=hello', got: %s", out)
	}
	if json.Valid(buf.Bytes()) {
		t.Errorf("expected non-JSON text output for TTY, got valid JSON: %s", out)
	}
}

func TestNew_NonTTY_UsesJSONHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo, false)

	logger.Info("hello", "key", "value")

	if !json.Valid(buf.Bytes()) {
		t.Errorf("expected valid JSON output for non-TTY, got: %s", buf.String())
	}
}

func TestNew_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelWarn, false)

	logger.Info("should be suppressed")
	logger.Warn("should appear")

	out := buf.String()
	if strings.Contains(out, "should be suppressed") {
		t.Errorf("expected info message to be suppressed below warn level, got: %s", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Errorf("expected warn message to appear, got: %s", out)
	}
}

func TestParseLevel_Valid(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	for input, want := range cases {
		got, err := ParseLevel(input)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseLevel_Empty_DefaultsToInfo(t *testing.T) {
	got, err := ParseLevel("")
	if err != nil {
		t.Fatalf("ParseLevel(\"\"): %v", err)
	}
	if got != slog.LevelInfo {
		t.Errorf("ParseLevel(\"\") = %v, want %v", got, slog.LevelInfo)
	}
}

func TestParseLevel_Invalid_FailsLoud(t *testing.T) {
	_, err := ParseLevel("not-a-level")
	if err == nil {
		t.Fatal("expected error for invalid level, got nil")
	}
}
