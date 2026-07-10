package slogx

import (
	"log/slog"
	"strings"
)

// ParseLevel maps a log-level string to an slog.Level. It is case-insensitive,
// trims surrounding space, and maps the long-form "warning" to "warn" (which
// slog.Level.UnmarshalText does not accept); otherwise it delegates to
// UnmarshalText, so offset syntax such as "warn+1" or "debug-2" also works.
//
// An empty string returns def with ok=true — an unset level is not an error. A
// non-empty unparseable value returns def with ok=false, so the caller can warn.
// Parse the level BEFORE installing the handler (via Setup), then warn on
// ok==false afterward, so the warning is emitted through the configured handler.
func ParseLevel(raw string, def slog.Level) (level slog.Level, ok bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return def, true
	}
	if s == "warning" {
		s = "warn"
	}
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(s)); err != nil {
		return def, false
	}
	return parsed, true
}
