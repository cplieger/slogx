package slogx

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func BenchmarkParseLevel(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = ParseLevel("warning", slog.LevelInfo)
	}
}

func BenchmarkNewHandler(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = NewHandler(Options{Output: io.Discard})
	}
}

func BenchmarkUTCTime(b *testing.B) {
	attr := slog.Time(slog.TimeKey, time.Now())
	b.ReportAllocs()
	for b.Loop() {
		_ = UTCTime(nil, attr)
	}
}
