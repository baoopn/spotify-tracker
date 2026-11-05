// Package logging configures the process-wide structured logger.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New builds a slog.Logger. format is "json" (default) or "text".
func New(service, level, format string) *slog.Logger {
	return newWithWriter(os.Stdout, service, level, format)
}

func newWithWriter(w io.Writer, service, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	var h slog.Handler
	if strings.EqualFold(format, "text") {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	l := slog.New(h).With(slog.String("service", service))
	slog.SetDefault(l)
	return l
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
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

// Err returns a slog attribute for an error.
func Err(err error) slog.Attr { return slog.Any("error", err) }
