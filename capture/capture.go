// Package capture provides a slog.Handler that records log records for
// assertion in tests, plus helpers to install it as the default logger.
//
// It is a separate package from slogx on purpose: the testing import and the
// record buffer are test-support machinery that should never reach slogx's
// production consumers. Import it only from _test.go files.
//
// Attributes added through Logger.With / WithGroup are not captured; assert on
// the level, message, and attributes passed directly to the log call. Use
// Records for anything the convenience methods do not cover.
package capture

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Recorder is a slog.Handler that captures every record it receives, at any
// level, so a test can assert on what was logged. It is safe for concurrent use.
type Recorder struct {
	records []slog.Record
	mu      sync.Mutex
}

// New returns a Recorder and an *slog.Logger that writes to it, for code that
// takes an injected logger. It does not touch the global default logger, so a
// test using it may run in parallel.
func New() (*slog.Logger, *Recorder) {
	rec := &Recorder{}
	return slog.New(rec), rec
}

// Default installs a fresh Recorder as slog's default logger and restores the
// previous default when the test ends (via tb.Cleanup). Use it for code that
// logs through slog.Default(). Because it mutates global state, a test using it
// must NOT call t.Parallel.
func Default(tb testing.TB) *Recorder {
	tb.Helper()
	rec := &Recorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	tb.Cleanup(func() { slog.SetDefault(prev) })
	return rec
}

// Enabled reports true for every level so nothing is filtered before capture.
func (rec *Recorder) Enabled(context.Context, slog.Level) bool { return true }

// Handle records a clone of r. It satisfies slog.Handler.
//
//nolint:gocritic // Handle's signature is fixed by the slog.Handler interface; r cannot be a pointer.
func (rec *Recorder) Handle(_ context.Context, r slog.Record) error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.records = append(rec.records, r.Clone())
	return nil
}

// WithAttrs returns the Recorder unchanged; base attributes are not captured.
func (rec *Recorder) WithAttrs([]slog.Attr) slog.Handler { return rec }

// WithGroup returns the Recorder unchanged; groups are not captured.
func (rec *Recorder) WithGroup(string) slog.Handler { return rec }

// Records returns a snapshot copy of the captured records, in order. Mutating
// or extending the result does not affect later captures.
func (rec *Recorder) Records() []slog.Record {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return slices.Clone(rec.records)
}

// Len returns the number of captured records.
func (rec *Recorder) Len() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.records)
}

// Messages returns the Message of each captured record, in order.
func (rec *Recorder) Messages() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	msgs := make([]string, len(rec.records))
	for i := range rec.records {
		msgs[i] = rec.records[i].Message
	}
	return msgs
}

// Contains reports whether any captured record's Message contains sub.
func (rec *Recorder) Contains(sub string) bool {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i := range rec.records {
		if strings.Contains(rec.records[i].Message, sub) {
			return true
		}
	}
	return false
}

// Count returns how many captured records have a Message containing sub.
func (rec *Recorder) Count(sub string) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for i := range rec.records {
		if strings.Contains(rec.records[i].Message, sub) {
			n++
		}
	}
	return n
}
