// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

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

// P2PSyncDebug emits verbose P2P, header sync, and block-download diagnostics.
// It is a no-op unless DebugMode is true (CLI -debug).
func P2PSyncDebug(msg string, args ...any) {
	if !DebugMode {
		return
	}
	a := append([]any{"component", "p2p_sync"}, args...)
	L.Debug(msg, a...)
}

// ChainSyncDebug emits verbose chain, header-index, reorg, and fork diagnostics.
// It is a no-op unless DebugMode is true (CLI -debug).
func ChainSyncDebug(msg string, args ...any) {
	if !DebugMode {
		return
	}
	a := append([]any{"component", "chain_sync"}, args...)
	L.Debug(msg, a...)
}

// SyncAuditDebug records high-signal sync/IBD decisions (why we are syncing, why
// we dropped a message, header/body phase transitions). Grep for component=sync_audit
// in logs to verify chain convergence behavior on mainnet review.
// No-op unless DebugMode is true (CLI -debug).
func SyncAuditDebug(msg string, args ...any) {
	if !DebugMode {
		return
	}
	a := append([]any{"component", "sync_audit"}, args...)
	L.Debug(msg, a...)
}

// With returns a child logger with additional default attributes.
func With(args ...any) *slog.Logger {
	return L.With(args...)
}
