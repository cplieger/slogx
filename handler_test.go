package slogx

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
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
	var buf bytes.Buffer
	handler, _ := NewHandler(Options{Output: &buf})
	zone := time.FixedZone("plusfive", 5*60*60)
	record := slog.NewRecord(time.Date(2026, 7, 10, 12, 0, 0, 0, zone), slog.LevelInfo, "hi", 0)

	if err := handler.Handle(t.Context(), record); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	timeField, _, _ := strings.Cut(buf.String(), " ")
	if timeField != "time=2026-07-10T07:00:00.000Z" {
		t.Errorf("time field = %q, want UTC-normalized %q", timeField, "time=2026-07-10T07:00:00.000Z")
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

func TestNewHandlerInvalidFormatPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewHandler with an out-of-range Format did not panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "invalid Format value 99") {
			t.Errorf("panic value = %v, want a string naming the invalid Format value", r)
		}
	}()
	NewHandler(Options{Format: Format(99)})
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

func TestSetupLevelVarControlsInstalledLogger(t *testing.T) {
	// Not parallel: mutates the global slog default. Restore it afterward.
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	var buf bytes.Buffer
	lv := Setup(Options{Output: &buf})

	slog.Debug("suppressed")
	if buf.Len() != 0 {
		t.Fatalf("Debug emitted at the Info default: %q", buf.String())
	}

	lv.Set(slog.LevelDebug)
	slog.Debug("now-visible")
	if !strings.Contains(buf.String(), "msg=now-visible") {
		t.Errorf("the LevelVar Setup returned does not control the installed logger: %q", buf.String())
	}
}

func TestNewHandlerNilOutputDefaultsToStderr(t *testing.T) {
	// Not parallel: temporarily swaps the global os.Stderr.
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })
	t.Cleanup(func() { _ = r.Close() })

	handler, _ := NewHandler(Options{}) // nil Output must default to os.Stderr
	slog.New(handler).Info("to-stderr")

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if !strings.Contains(string(out), "msg=to-stderr") {
		t.Errorf("nil Output did not default to os.Stderr; captured %q", out)
	}
}
