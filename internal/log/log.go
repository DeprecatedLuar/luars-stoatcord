package log

import (
	"fmt"
	"io"
	"log/slog"
)

// New builds a *slog.Logger writing to w. It uses a text handler when isTTY
// is true (foreground/dev), JSON otherwise (e.g. systemd journal). The
// logger is returned for injection into components; no global is set.
func New(w io.Writer, level slog.Level, isTTY bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if isTTY {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(handler)
}

// ParseLevel parses a LOG_LEVEL string (debug|info|warn|error, case
// insensitive). An empty string defaults to info. Any other value fails loud.
func ParseLevel(s string) (slog.Level, error) {
	if s == "" {
		return slog.LevelInfo, nil
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("log: invalid LOG_LEVEL %q: %w", s, err)
	}
	return level, nil
}
