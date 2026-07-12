package slogx

import (
	"log/slog"
	"strings"
	"testing"
)

// FuzzParseLevel asserts the invariants every caller relies on against arbitrary
// LOG_LEVEL values: it never panics, a rejected value returns the default
// unchanged, and a blank value is always the default with ok=true.
func FuzzParseLevel(f *testing.F) {
	for _, seed := range []string{
		"", "  ", "debug", "info", "warn", "warning", "WARNING",
		"error", "warn+1", "debug-2", "banana", "warn ", "12", "info+99",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		level, ok := ParseLevel(raw, slog.LevelInfo)
		if !ok && level != slog.LevelInfo {
			t.Fatalf("ParseLevel(%q) not ok but level=%v, want the default LevelInfo", raw, level)
		}
		if strings.TrimSpace(raw) == "" && (!ok || level != slog.LevelInfo) {
			t.Fatalf("ParseLevel(%q) = (%v, %v), want (Info, true) for a blank value", raw, level, ok)
		}
		// A recognized value must round-trip: re-parsing its canonical string
		// yields the same level (idempotence), independent of the default given.
		if ok && strings.TrimSpace(raw) != "" {
			if reparsed, reok := ParseLevel(level.String(), slog.LevelError); !reok || reparsed != level {
				t.Fatalf("ParseLevel(%q)=%v did not round-trip: ParseLevel(%q, Error)=(%v, %v)", raw, level, level.String(), reparsed, reok)
			}
		}
	})
}
