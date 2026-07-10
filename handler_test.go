package slogx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewHandlerTextToBuffer(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	handler, lv := NewHandler(Options{Output: &buf})
	if lv.Level() != slog.LevelInfo {
		t.Errorf("default level = %v, want Info", lv.Level())
	}
	slog.New(handler).Info("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "level=INFO") || !strings.Contains(out, "msg=hello") || !strings.Contains(out, "k=v") {
		t.Errorf("text output missing expected fields: %q", out)
	}
}

func TestNewHandlerJSONToBuffer(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	handler, _ := NewHandler(Options{Output: &buf, Format: JSON})
	slog.New(handler).Info("hello")
	out := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(out, "{") || !strings.Contains(out, `"msg":"hello"`) {
		t.Errorf("JSON output is not JSON-shaped: %q", out)
	}
}

func TestNewHandlerUTCNormalizesTime(t *testing.T) {
	t.Parallel()
	// UTCTime is wired, so the emitted timestamp is UTC — RFC3339 renders that
	// with a trailing Z even when the host TZ has a non-zero offset (a missing
	// UTCTime on such a host would render a +hh:mm offset instead).
	var buf bytes.Buffer
	handler, _ := NewHandler(Options{Output: &buf})
	slog.New(handler).Info("hi")
	timeField, _, _ := strings.Cut(buf.String(), " ")
	if !strings.HasPrefix(timeField, "time=") || !strings.HasSuffix(timeField, "Z") {
		t.Errorf("time field %q is not a UTC (…Z) timestamp", timeField)
	}
}

func TestNewHandlerLevelFiltering(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	handler, lv := NewHandler(Options{Output: &buf, Level: slog.LevelWarn})
	logger := slog.New(handler)

	logger.Info("suppressed")
	if buf.Len() != 0 {
		t.Errorf("Info emitted below the Warn threshold: %q", buf.String())
	}
	logger.Warn("shown")
	if !strings.Contains(buf.String(), "msg=shown") {
		t.Errorf("Warn not emitted at the Warn threshold: %q", buf.String())
	}

	// The returned LevelVar controls the live level.
	buf.Reset()
	lv.Set(slog.LevelDebug)
	logger.Debug("now-visible")
	if !strings.Contains(buf.String(), "msg=now-visible") {
		t.Errorf("Debug not emitted after lowering the level via the LevelVar: %q", buf.String())
	}
}

func TestNewHandlerAddSource(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	handler, _ := NewHandler(Options{Output: &buf, AddSource: true})
	slog.New(handler).Info("hi")
	if !strings.Contains(buf.String(), "source=") {
		t.Errorf("AddSource did not add a source attr: %q", buf.String())
	}
}

func TestSetupInstallsDefault(t *testing.T) {
	// Not parallel: mutates the global slog default. Restore it afterward.
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	var buf bytes.Buffer
	lv := Setup(Options{Output: &buf})
	if lv == nil {
		t.Fatal("Setup returned a nil LevelVar")
	}
	slog.Info("through-default", "k", "v")
	if !strings.Contains(buf.String(), "msg=through-default") {
		t.Errorf("Setup did not install the default logger: %q", buf.String())
	}
}
