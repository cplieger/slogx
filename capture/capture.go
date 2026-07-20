// Package capture provides a slog.Handler that records log records for
// assertion in tests, plus helpers to install it as the default logger.
//
// It is a separate package from slogx on purpose: the testing import and the
// record buffer are test-support machinery that should never reach slogx's
// production consumers. Import it only from _test.go files.
//
// Attributes and groups added through Logger.With / Logger.WithGroup are
// captured with the same nesting a real handler would emit: derived handles
// share the recorder's buffer and materialize their inherited context into
// each stored record, so Records reflects what production output would
// contain. Use Records for anything the convenience methods do not cover.
//
// Captured records are render-faithful: they hold what a stdlib TextHandler
// or JSONHandler would emit, not the verbatim call-site input. At ingestion
// the recorder applies the attribute output rules of the slog.Handler
// contract — values are resolved (slog.LogValuer), a zero Attr is dropped, a
// group with no attrs is dropped, and an empty-keyed group has its attrs
// inlined into its parent.
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
// level, so a test can assert on what was logged. Handler derivation is
// honored per the slog.Handler contract: handles returned by WithAttrs and
// WithGroup record through the same Recorder, with their inherited attributes
// and groups materialized into each stored record. It is safe for concurrent
// use.
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

// Handle records r as a stdlib handler would emit it: attribute values are
// resolved and degenerate attrs are dropped or inlined per the slog.Handler
// contract (see normalizeAttrs). Time, Level, Message, and PC are stored
// unchanged. It satisfies slog.Handler.
//
//nolint:gocritic // Handle's signature is fixed by the slog.Handler interface; r cannot be a pointer.
func (rec *Recorder) Handle(_ context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	nr.AddAttrs(normalizedRecordAttrs(&r)...)
	rec.append(&nr)
	return nil
}

// append stores one materialized record. All handles derived from this
// Recorder funnel through it, so the buffer stays ordered and race-free.
func (rec *Recorder) append(r *slog.Record) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.records = append(rec.records, *r)
}

// WithAttrs returns a handler that records through this Recorder with attrs
// prepended to every captured record, exactly as a real handler would include
// them. Like the stdlib handlers, it normalizes attrs at derivation time (see
// normalizeAttrs); a slice that is empty or normalizes to empty returns the
// receiver.
func (rec *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = normalizeAttrs(attrs)
	if len(attrs) == 0 {
		return rec
	}
	return &derived{rec: rec, ops: []op{{attrs: attrs}}}
}

// WithGroup returns a handler that records through this Recorder with
// subsequent attributes nested under name, exactly as a real handler would
// qualify them. An empty name returns the receiver, per the slog.Handler
// contract.
func (rec *Recorder) WithGroup(name string) slog.Handler {
	if name == "" {
		return rec
	}
	return &derived{rec: rec, ops: []op{{group: name}}}
}

// op is one derivation step: a WithAttrs attribute set (attrs non-empty) or a
// WithGroup name (group non-empty). Exactly one field is set.
type op struct {
	group string
	attrs []slog.Attr
}

// derived is the handle WithAttrs/WithGroup return: an immutable derivation
// prefix over the root Recorder's shared record buffer.
type derived struct {
	rec *Recorder
	ops []op
}

// Enabled reports true for every level so nothing is filtered before capture.
func (d *derived) Enabled(context.Context, slog.Level) bool { return true }

// Handle materializes the derivation prefix into the record — inherited
// attributes at their nesting depth, later attributes and the record's own
// under every group opened before them — and stores the result, so the
// captured record matches what a real handler would emit. The record's own
// attrs are normalized first (see normalizeAttrs), so an enclosing group
// whose content normalizes to nothing is elided like the stdlib handlers
// elide it.
//
//nolint:gocritic // Handle's signature is fixed by the slog.Handler interface; r cannot be a pointer.
func (d *derived) Handle(_ context.Context, r slog.Record) error {
	attrs := normalizedRecordAttrs(&r)
	for _, o := range slices.Backward(d.ops) {
		switch {
		case o.group != "":
			if len(attrs) == 0 {
				continue // a group holding no attrs is elided, like the stdlib handlers do
			}
			attrs = []slog.Attr{{Key: o.group, Value: slog.GroupValue(attrs...)}}
		default:
			attrs = slices.Concat(o.attrs, attrs)
		}
	}
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	nr.AddAttrs(attrs...)
	d.rec.append(&nr)
	return nil
}

// WithAttrs returns a new handle extending the derivation with attrs,
// normalized at derivation time exactly as Recorder.WithAttrs normalizes
// them. A slice that is empty or normalizes to empty returns the receiver.
func (d *derived) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = normalizeAttrs(attrs)
	if len(attrs) == 0 {
		return d
	}
	return &derived{rec: d.rec, ops: appendOp(d.ops, op{attrs: attrs})}
}

// WithGroup returns a new handle extending the derivation with a group. An
// empty name returns the receiver, per the slog.Handler contract.
func (d *derived) WithGroup(name string) slog.Handler {
	if name == "" {
		return d
	}
	return &derived{rec: d.rec, ops: appendOp(d.ops, op{group: name})}
}

// appendOp extends a derivation prefix into fresh backing storage, so sibling
// handles derived from the same parent never alias each other's steps.
func appendOp(ops []op, next op) []op {
	out := make([]op, len(ops)+1)
	copy(out, ops)
	out[len(ops)] = next
	return out
}

// normalizedRecordAttrs collects r's attributes and passes them through
// normalizeAttrs, yielding the attr list a stdlib handler would render for r.
func normalizedRecordAttrs(r *slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return normalizeAttrs(attrs)
}

// normalizeAttrs applies the attribute output rules of the slog.Handler
// contract, returning the attrs a stdlib handler would render:
//
//   - every value is resolved first (slog.LogValuer chains; Value.Resolve is
//     loop-capped);
//   - a group's attrs are normalized recursively, and a group left with no
//     attrs is dropped — emptiness is judged after inner normalization, so a
//     group holding only degenerate attrs drops too;
//   - an empty-keyed group has its normalized attrs inlined into its parent;
//   - a zero Attr (key and value both zero) is dropped.
//
// Group values are rebuilt via slog.GroupValue on freshly allocated slices,
// so normalized output never aliases caller-owned storage: content is fixed
// at ingestion time the way a real handler's rendered output is.
func normalizeAttrs(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		a.Value = a.Value.Resolve()
		if a.Value.Kind() == slog.KindGroup {
			children := normalizeAttrs(a.Value.Group())
			switch {
			case len(children) == 0: // a group with no attrs is ignored
			case a.Key == "": // an empty-keyed group inlines its attrs
				out = append(out, children...)
			default:
				out = append(out, slog.Attr{Key: a.Key, Value: slog.GroupValue(children...)})
			}
			continue
		}
		if a.Equal(slog.Attr{}) {
			continue // a zero Attr is ignored
		}
		out = append(out, a)
	}
	return out
}

// Records returns a snapshot copy of the captured records, in order. Mutating
// or extending the result does not affect later captures.
func (rec *Recorder) Records() []slog.Record {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	records := make([]slog.Record, len(rec.records))
	for i := range rec.records {
		records[i] = cloneRecord(&rec.records[i])
	}
	return records
}

// cloneRecord deep-copies a record so the snapshot's group values get fresh
// backing storage: slog.Record.Clone does not recursively copy KindGroup
// values, whose Group() result aliases the stored record's mutable slice.
func cloneRecord(r *slog.Record) slog.Record {
	clone := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clone.AddAttrs(cloneAttr(a))
		return true
	})
	return clone
}

// cloneAttr returns an Attr with any group value (recursively) rebuilt on
// fresh backing storage; non-group attrs are returned as-is.
func cloneAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() != slog.KindGroup {
		return a
	}
	a.Value = slog.GroupValue(cloneAttrs(a.Value.Group())...)
	return a
}

// cloneAttrs deep-copies a slice of attrs onto fresh backing storage via
// cloneAttr, so a snapshot's content is isolated from the recorder's stored
// records.
func cloneAttrs(attrs []slog.Attr) []slog.Attr {
	cloned := make([]slog.Attr, len(attrs))
	for i := range attrs {
		cloned[i] = cloneAttr(attrs[i])
	}
	return cloned
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
	return rec.Count(sub) > 0
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

// CountExact returns how many captured records have a Message exactly equal to
// msg. Use it over Count when the message is pinned by an external contract
// (for example a Loki alert rule matching the exact msg value): the substring
// Count would also match a superstring message ("cycle completed with errors"
// matches Count("cycle complete")), silently passing a test the contract
// should fail.
func (rec *Recorder) CountExact(msg string) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for i := range rec.records {
		if rec.records[i].Message == msg {
			n++
		}
	}
	return n
}
