// Package logging configures OmaStore's default slog logger.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Setup installs a text handler on w at the level given by level
// ("debug", "info", "warn", "error"). If level is empty, OMASTORE_LOG is read.
func Setup(w io.Writer, level string) *slog.Logger {
	if level == "" {
		level = os.Getenv("OMASTORE_LOG")
	}
	l := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: ParseLevel(level)}))
	slog.SetDefault(l)
	return l
}

// ParseLevel converts a name into a slog.Level; the default is Info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
