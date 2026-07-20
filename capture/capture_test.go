package capture

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestNewCapturesInjectedLogger(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	logger.Info("hello", "k", "v")
	logger.Warn("second")

	if rec.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", rec.Len())
	}
	if !rec.Contains("hello") {
		t.Error("Contains(hello) = false, want true")
	}
	if got := rec.Count("second"); got != 1 {
		t.Errorf("Count(second) = %d, want 1", got)
	}
	if msgs := rec.Messages(); len(msgs) != 2 || msgs[0] != "hello" || msgs[1] != "second" {
		t.Errorf("Messages() = %v, want [hello second]", msgs)
	}
}

func TestRecordsReturnsSnapshotCopy(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	logger.Info("one")
	snap := rec.Records()
	logger.Info("two")
	if len(snap) != 1 {
		t.Errorf("snapshot len = %d, want 1 (a later log must not grow an earlier snapshot)", len(snap))
	}
}

func TestRecordsSnapshotGroupsAreIsolated(t *testing.T) {
	t.Parallel()
	// A snapshot's group values must have fresh backing storage: mutating a
	// group slice fetched from one Records() call must not be visible in the
	// stored record or a later snapshot.
	logger, rec := New()
	logger.WithGroup("g").Info("m", "k", "before")

	first := rec.Records()
	grp := recordAttrs(t, first[0])[0].Value.Group()
	grp[0] = slog.String("k", "mutated")

	second := rec.Records()
	got := recordAttrs(t, second[0])[0].Value.Group()
	if len(got) != 1 || got[0].Key != "k" || got[0].Value.String() != "before" {
		t.Errorf("second snapshot group = %v, want [k=before] (snapshot mutation leaked into the recorder)", got)
	}
}

func TestRecordsSnapshotNestedGroupsAreIsolated(t *testing.T) {
	t.Parallel()
	// cloneAttr must rebuild group values RECURSIVELY: mutating an inner
	// (nested) group slice fetched from one Records() call must not be visible
	// in a later snapshot. A shallow one-level copy would pass the flat-group
	// isolation test but alias the nested slice.
	logger, rec := New()
	logger.WithGroup("outer").WithGroup("inner").Info("m", "k", "before")

	first := rec.Records()
	outer := recordAttrs(t, first[0])[0].Value.Group()
	innerGrp := outer[0].Value.Group()
	innerGrp[0] = slog.String("k", "mutated")

	second := rec.Records()
	got := recordAttrs(t, second[0])[0].Value.Group()[0].Value.Group()
	if len(got) != 1 || got[0].Key != "k" || got[0].Value.String() != "before" {
		t.Errorf("second snapshot nested group = %v, want [k=before] (nested-group snapshot mutation leaked into the recorder)", got)
	}
}

func TestRecordsSnapshotPreservesRecordFields(t *testing.T) {
	t.Parallel()
	// cloneRecord rebuilds each record via slog.NewRecord; the snapshot must
	// carry the original Time, Level, and PC through that rebuild.
	logger, rec := New()
	before := time.Now()
	logger.Error("boom", "k", "v")
	after := time.Now()

	records := rec.Records()
	if len(records) != 1 {
		t.Fatalf("Len = %d, want 1", len(records))
	}
	r := records[0]
	if r.Level != slog.LevelError {
		t.Errorf("snapshot Level = %v, want Error", r.Level)
	}
	if r.Time.Before(before) || r.Time.After(after) {
		t.Errorf("snapshot Time = %v, want within [%v, %v]", r.Time, before, after)
	}
	if r.PC == 0 {
		t.Error("snapshot PC = 0, want the original call-site PC preserved")
	}
	if r.Message != "boom" {
		t.Errorf("snapshot Message = %q, want %q", r.Message, "boom")
	}
	if n := r.NumAttrs(); n != 1 {
		t.Errorf("snapshot NumAttrs = %d, want 1", n)
	}
}

func TestStoredRecordsDoNotAliasCallerGroupSlices(t *testing.T) {
	t.Parallel()
	// A real handler fixes rendered content at Handle time; mutating a group
	// slice the caller retained after logging must not change what was captured.
	logger, rec := New()
	kids := []slog.Attr{slog.String("k", "before")}
	logger.LogAttrs(context.Background(), slog.LevelInfo, "m",
		slog.Attr{Key: "g", Value: slog.GroupValue(kids...)})

	kids[0] = slog.String("k", "after")

	got := recordAttrs(t, rec.Records()[0])[0].Value.Group()
	if len(got) != 1 || got[0].Value.String() != "before" {
		t.Errorf("captured group = %v, want [k=before] (stored record aliased the caller's slice)", got)
	}
}

func TestDerivedRecordsDoNotAliasCallerGroupSlices(t *testing.T) {
	t.Parallel()

	t.Run("inherited attrs are fixed at derivation time", func(t *testing.T) {
		logger, rec := New()
		children := []slog.Attr{slog.String("k", "before")}
		derived := logger.With(slog.Attr{Key: "g", Value: slog.GroupValue(children...)})

		children[0] = slog.String("k", "after")
		derived.Info("m")

		got := recordAttrs(t, rec.Records()[0])[0].Value.Group()
		if len(got) != 1 || got[0].Value.String() != "before" {
			t.Errorf("captured inherited group = %v, want [k=before] (derived attrs aliased the caller's slice)", got)
		}
	})

	t.Run("call-site attrs are fixed at handle time", func(t *testing.T) {
		logger, rec := New()
		derived := logger.With("base", 1)
		children := []slog.Attr{slog.String("k", "before")}

		derived.LogAttrs(context.Background(), slog.LevelInfo, "m",
			slog.Attr{Key: "g", Value: slog.GroupValue(children...)})
		children[0] = slog.String("k", "after")

		got := recordAttrs(t, rec.Records()[0])[1].Value.Group()
		if len(got) != 1 || got[0].Value.String() != "before" {
			t.Errorf("captured call-site group = %v, want [k=before] (derived Handle aliased the caller's slice)", got)
		}
	})
}

func TestWithAttrsAndGroupStillCapture(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	// WithAttrs/WithGroup derive handles over the same record buffer; the
	// record lands on the root Recorder.
	logger.With("base", 1).WithGroup("grp").Info("nested")
	if !rec.Contains("nested") {
		t.Error("WithAttrs/WithGroup dropped the record")
	}
}

// recordAttrs flattens a record's top-level attrs for structural assertions.
func recordAttrs(t *testing.T, r slog.Record) []slog.Attr {
	t.Helper()
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}

func TestWithAttrsCapturedAtTopLevel(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	logger.With("base", 1).Info("m", "direct", "v")

	records := rec.Records()
	if len(records) != 1 {
		t.Fatalf("Len = %d, want 1", len(records))
	}
	attrs := recordAttrs(t, records[0])
	if len(attrs) != 2 {
		t.Fatalf("attrs = %v, want [base=1 direct=v]", attrs)
	}
	if attrs[0].Key != "base" || attrs[0].Value.String() != "1" {
		t.Errorf("inherited attr = %v, want base=1 first (inherited attrs precede call-site attrs)", attrs[0])
	}
	if attrs[1].Key != "direct" || attrs[1].Value.String() != "v" {
		t.Errorf("call-site attr = %v, want direct=v", attrs[1])
	}
}

func TestWithGroupNestingMatchesRealHandler(t *testing.T) {
	t.Parallel()
	// logger.With(a).WithGroup(g).With(b) logging (m, c) must capture
	// a=1 at the top level and b=2, c=3 inside group g — the same shape a
	// stdlib handler renders as a=1 g.b=2 g.c=3.
	logger, rec := New()
	logger.With("a", 1).WithGroup("g").With("b", 2).Info("m", "c", 3)

	records := rec.Records()
	if len(records) != 1 {
		t.Fatalf("Len = %d, want 1", len(records))
	}
	attrs := recordAttrs(t, records[0])
	if len(attrs) != 2 {
		t.Fatalf("top-level attrs = %v, want [a g]", attrs)
	}
	if attrs[0].Key != "a" || attrs[0].Value.String() != "1" {
		t.Errorf("attrs[0] = %v, want a=1", attrs[0])
	}
	if attrs[1].Key != "g" || attrs[1].Value.Kind() != slog.KindGroup {
		t.Fatalf("attrs[1] = %v, want group g", attrs[1])
	}
	grp := attrs[1].Value.Group()
	if len(grp) != 2 || grp[0].Key != "b" || grp[0].Value.String() != "2" || grp[1].Key != "c" || grp[1].Value.String() != "3" {
		t.Errorf("group g = %v, want [b=2 c=3]", grp)
	}
}

func TestNestedGroupsCaptured(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	logger.WithGroup("g").WithGroup("h").Info("m", "k", 1)

	attrs := recordAttrs(t, rec.Records()[0])
	if len(attrs) != 1 || attrs[0].Key != "g" || attrs[0].Value.Kind() != slog.KindGroup {
		t.Fatalf("top-level attrs = %v, want a single group g", attrs)
	}
	inner := attrs[0].Value.Group()
	if len(inner) != 1 || inner[0].Key != "h" || inner[0].Value.Kind() != slog.KindGroup {
		t.Fatalf("g = %v, want a single nested group h", inner)
	}
	leaf := inner[0].Value.Group()
	if len(leaf) != 1 || leaf[0].Key != "k" || leaf[0].Value.String() != "1" {
		t.Errorf("g.h = %v, want [k=1]", leaf)
	}
}

func TestEmptyGroupElided(t *testing.T) {
	t.Parallel()
	// A group with no attrs inside it is elided, matching stdlib handler output.
	logger, rec := New()
	logger.WithGroup("g").Info("bare")

	r := rec.Records()[0]
	if n := r.NumAttrs(); n != 0 {
		t.Errorf("NumAttrs = %d, want 0 (empty group must be elided)", n)
	}
}

func TestDerivedHandleContractEdges(t *testing.T) {
	t.Parallel()
	_, rec := New()
	// Contract: an empty name / empty attrs slice returns the receiver.
	if got := rec.WithGroup(""); got != slog.Handler(rec) {
		t.Error("Recorder.WithGroup(\"\") did not return the receiver")
	}
	if got := rec.WithAttrs(nil); got != slog.Handler(rec) {
		t.Error("Recorder.WithAttrs(nil) did not return the receiver")
	}
	d := rec.WithAttrs([]slog.Attr{slog.Int("a", 1)})
	if got := d.WithGroup(""); got != d {
		t.Error("derived.WithGroup(\"\") did not return the receiver")
	}
	if got := d.WithAttrs(nil); got != d {
		t.Error("derived.WithAttrs(nil) did not return the receiver")
	}
}

func TestDerivedSiblingsDoNotAlias(t *testing.T) {
	t.Parallel()
	// Two handles derived from the same parent must not leak steps into each
	// other's prefix (a fresh-backing-array regression guard).
	logger, rec := New()
	parent := logger.With("p", 0)
	parent.With("a", 1).Info("via-a")
	parent.With("b", 2).Info("via-b")

	records := rec.Records()
	if len(records) != 2 {
		t.Fatalf("Len = %d, want 2", len(records))
	}
	attrsA := recordAttrs(t, records[0])
	if len(attrsA) != 2 || attrsA[0].Key != "p" || attrsA[1].Key != "a" {
		t.Errorf("first record attrs = %v, want [p a]", attrsA)
	}
	attrsB := recordAttrs(t, records[1])
	if len(attrsB) != 2 || attrsB[0].Key != "p" || attrsB[1].Key != "b" {
		t.Errorf("second record attrs = %v, want [p b] (sibling must not inherit a)", attrsB)
	}
}

func TestConcurrentDerivedHandlesAreRaceFree(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	var wg sync.WaitGroup
	for i := range 25 {
		wg.Go(func() { logger.With("worker", i).Info("derived-concurrent") })
		wg.Go(func() { logger.Info("root-concurrent") })
	}
	wg.Wait()
	if got := rec.Count("derived-concurrent"); got != 25 {
		t.Errorf("Count(derived-concurrent) = %d, want 25", got)
	}
	if got := rec.Count("root-concurrent"); got != 25 {
		t.Errorf("Count(root-concurrent) = %d, want 25", got)
	}
}

func TestDefaultCapturesGlobalAndRestores(t *testing.T) {
	// Not parallel: mutates the global slog default.
	before := slog.Default()
	t.Run("captures", func(t *testing.T) {
		rec := Default(t)
		slog.Info("through-default")
		if !rec.Contains("through-default") {
			t.Error("Default did not capture a slog.Default() log")
		}
	})
	if slog.Default() != before {
		t.Error("Default did not restore slog.Default() after the subtest ended")
	}
}

func TestConcurrentHandleIsRaceFree(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { logger.Info("concurrent") })
	}
	wg.Wait()
	if got := rec.Count("concurrent"); got != 50 {
		t.Errorf("Count(concurrent) = %d, want 50", got)
	}
}

func TestRecorderNegativeLookups(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	logger.Info("present")

	if rec.Contains("absent") {
		t.Error("Contains(absent) = true, want false for a substring no record holds")
	}
	if got := rec.Count("absent"); got != 0 {
		t.Errorf("Count(absent) = %d, want 0", got)
	}
}

func TestCaptureRecordsBelowDefaultLevel(t *testing.T) {
	t.Parallel()
	// Recorder.Enabled reports true for every level, so a Debug record (below
	// slog's default Info threshold) is still captured.
	logger, rec := New()
	logger.Debug("debug-detail")
	if !rec.Contains("debug-detail") {
		t.Error("Debug record not captured; Recorder must record at any level")
	}
}

func TestCountExact(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	logger.Info("cycle complete")
	logger.Warn("cycle complete")
	logger.Error("cycle completed with errors")
	logger.Info("prefix cycle complete")

	// The exact-match count sees only the two exact messages; the substring
	// Count sees all four — the false-pass window CountExact exists to close.
	if got := rec.CountExact("cycle complete"); got != 2 {
		t.Errorf("CountExact(cycle complete) = %d, want 2", got)
	}
	if got := rec.Count("cycle complete"); got != 4 {
		t.Errorf("Count(cycle complete) = %d, want 4 (substring semantics)", got)
	}
	if got := rec.CountExact("cycle"); got != 0 {
		t.Errorf("CountExact(cycle) = %d, want 0 for a partial message", got)
	}
	if got := rec.CountExact("absent"); got != 0 {
		t.Errorf("CountExact(absent) = %d, want 0", got)
	}
	if got := rec.CountExact(""); got != 0 {
		t.Errorf("CountExact(\"\") = %d, want 0 when no record has an empty message", got)
	}
}
