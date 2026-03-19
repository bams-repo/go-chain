package logging

import (
	"log/slog"
	"os"
)

var (
	L *slog.Logger

	// DebugMode is set by the -debug CLI flag. When true, subsystems emit
	// hyper-verbose diagnostic output covering block relay, peer topology,
	// sync state, and message flow. This goes beyond slog.LevelDebug by
	// enabling periodic dumps and per-message tracing.
	DebugMode bool
)

func init() {
	L = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// Init replaces the global logger with one configured at the given level and format.
// format may be "text" (default) or "json" for structured JSON output.
func Init(level string, format ...string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if len(format) > 0 && format[0] == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	L = slog.New(handler)
	slog.SetDefault(L)
}

// EnableDebug sets DebugMode and forces log level to debug.
func EnableDebug() {
	DebugMode = true
	L = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(L)
}

// With returns a child logger with additional default attributes.
func With(args ...any) *slog.Logger {
	return L.With(args...)
}
