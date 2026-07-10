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
