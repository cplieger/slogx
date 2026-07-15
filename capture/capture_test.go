package capture

import (
	"log/slog"
	"sync"
	"testing"
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

func TestWithAttrsAndGroupStillCapture(t *testing.T) {
	t.Parallel()
	logger, rec := New()
	// WithAttrs/WithGroup return the Recorder unchanged; the record still lands.
	logger.With("base", 1).WithGroup("grp").Info("nested")
	if !rec.Contains("nested") {
		t.Error("WithAttrs/WithGroup dropped the record")
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
