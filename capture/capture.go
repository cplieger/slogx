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
// use, and the zero value is ready to use: &Recorder{} is a working handler
// (New and Default are conveniences, not required constructors).
type Recorder struct {
	records []slog.Record
	mu      sync.Mutex
}

// New returns an *slog.Logger and the Recorder that captures its output, for code that
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
	nr := materialize(&r)
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

// materialize returns a copy of r rebuilt through normalizedRecordAttrs, so
// every group value sits on freshly allocated backing storage. Handle uses it
// to fix content at ingestion; Records reuses it for snapshot isolation,
// which is sound because normalizeAttrs is idempotent on already-normalized
// attrs (values are already resolved, degenerate attrs already dropped) and
// always rebuilds group values on fresh slices.
func materialize(r *slog.Record) slog.Record {
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	nr.AddAttrs(normalizedRecordAttrs(r)...)
	return nr
}

// Records returns a snapshot copy of the captured records, in order. Each
// record is rebuilt through materialize so its group values get fresh backing
// storage (slog.Record.Clone does not recursively copy KindGroup values,
// whose Group() result aliases the stored record's mutable slice); mutating
// or extending the result does not affect later captures.
func (rec *Recorder) Records() []slog.Record {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	records := make([]slog.Record, len(rec.records))
	for i := range rec.records {
		records[i] = materialize(&rec.records[i])
	}
	return records
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
	return rec.countMessages(func(msg string) bool { return strings.Contains(msg, sub) })
}

// countMessages returns how many captured records have a Message matching
// the given predicate. It is the shared scan behind Count and CountExact.
func (rec *Recorder) countMessages(match func(string) bool) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for i := range rec.records {
		if match(rec.records[i].Message) {
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
	return rec.countMessages(func(m string) bool { return m == msg })
}

// --- Attribute-level assertions ---
//
// The attr helpers below assert on a record's TOP-LEVEL attributes (the same
// set a handler receives after WithAttrs derivations are folded in); values
// nested inside groups are out of scope - walk Records() directly for those.
// Values compare by their RENDERED form (slog.Value.String()), so an
// assertion is kind-agnostic: an Int64 7 renders "7", a string "7" renders
// "7", and a test pinning a logged count does not care which the code chose.
// The empty string is a wildcard for both scoping parameters: msgSub ""
// matches every record, key "" matches every attribute.

// AttrValue returns the rendered value of the first top-level attribute
// named key on a captured record whose Message contains msgSub, in capture
// order, and whether one was found.
func (rec *Recorder) AttrValue(msgSub, key string) (string, bool) {
	var value string
	found := rec.scanAttrs(msgSub, key, func(v string) bool {
		value = v
		return true
	})
	return value, found
}

// HasAttr reports whether any captured record whose Message contains msgSub
// carries a top-level attribute named key whose rendered value equals
// rendered.
func (rec *Recorder) HasAttr(msgSub, key, rendered string) bool {
	return rec.scanAttrs(msgSub, key, func(v string) bool { return v == rendered })
}

// AttrContains reports whether any captured record whose Message contains
// msgSub carries a top-level attribute named key whose rendered value
// contains sub.
func (rec *Recorder) AttrContains(msgSub, key, sub string) bool {
	return rec.scanAttrs(msgSub, key, func(v string) bool { return strings.Contains(v, sub) })
}

// CountLevel returns how many captured records at exactly level have a
// Message containing msgSub ("" counts every record at that level). It
// exists for level-escalation contracts - a WARN log site that flips to
// ERROR past a threshold - where the assertion is precisely "one ERROR and
// zero WARN of this message", which the level-blind Count family cannot
// express. Message matching is by substring, the same vocabulary as Count,
// Contains, and the attr helpers' msgSub.
func (rec *Recorder) CountLevel(level slog.Level, msgSub string) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for i := range rec.records {
		if rec.records[i].Level == level && strings.Contains(rec.records[i].Message, msgSub) {
			n++
		}
	}
	return n
}

// scanAttrs is the shared walk behind the attr helpers: it visits captured
// records in order, scopes by Message-contains-msgSub and attr-key-equals-key
// (either "" = wildcard), and reports whether match accepted any rendered
// value. A true from match stops the scan. Records are stored materialized
// (Handle folds derivations in and drops degenerate attrs at ingestion), so
// the scan reads them in place; only rendered strings leave the lock, never
// a reference into the buffer, which is why it needs no Records()-style
// defensive copy.
func (rec *Recorder) scanAttrs(msgSub, key string, match func(rendered string) bool) bool {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i := range rec.records {
		if msgSub != "" && !strings.Contains(rec.records[i].Message, msgSub) {
			continue
		}
		found := false
		rec.records[i].Attrs(func(a slog.Attr) bool {
			if key != "" && a.Key != key {
				return true
			}
			if match(a.Value.String()) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}
