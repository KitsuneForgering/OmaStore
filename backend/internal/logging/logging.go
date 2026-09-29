// Package logging configura o slog padrão do OmaStore.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Setup instala um handler de texto em w com o nível dado por level
// ("debug", "info", "warn", "error"). Se level for vazio, lê OMASTORE_LOG.
func Setup(w io.Writer, level string) *slog.Logger {
	if level == "" {
		level = os.Getenv("OMASTORE_LOG")
	}
	l := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: ParseLevel(level)}))
	slog.SetDefault(l)
	return l
}

// ParseLevel converte um nome em slog.Level; o default é Info.
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
