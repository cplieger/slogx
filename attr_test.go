package slogx

import (
	"log/slog"
	"testing"
	"time"
)

func TestUTCTimeConvertsTopLevelTime(t *testing.T) {
	t.Parallel()
	// A time in a +05:00 zone must be rewritten to the same instant in UTC.
	zone := time.FixedZone("plusfive", 5*60*60)
	instant := time.Date(2026, 7, 10, 12, 0, 0, 0, zone)

	got := UTCTime(nil, slog.Time(slog.TimeKey, instant))

	gotTime := got.Value.Time()
	if _, offset := gotTime.Zone(); offset != 0 {
		t.Errorf("zone offset = %d, want 0 (UTC)", offset)
	}
	if !gotTime.Equal(instant) {
		t.Errorf("instant changed: got %v, want the same instant as %v", gotTime, instant)
	}
}

func TestUTCTimeLeavesNonTimeAttrs(t *testing.T) {
	t.Parallel()
	got := UTCTime(nil, slog.String("msg", "hello"))
	if got.Value.String() != "hello" {
		t.Errorf("non-time attr altered: got %q, want %q", got.Value.String(), "hello")
	}
}

func TestUTCTimeLeavesGroupedTimeKey(t *testing.T) {
	t.Parallel()
	// A "time"-keyed attr inside a group (groups non-empty) is a user attr, not
	// the record's built-in timestamp, so it must be left untouched.
	zone := time.FixedZone("plusfive", 5*60*60)
	instant := time.Date(2026, 7, 10, 12, 0, 0, 0, zone)

	got := UTCTime([]string{"grp"}, slog.Time(slog.TimeKey, instant))

	if _, offset := got.Value.Time().Zone(); offset != 5*60*60 {
		t.Errorf("grouped time attr offset = %d, want %d (untouched)", offset, 5*60*60)
	}
}
