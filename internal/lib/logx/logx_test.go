package logx

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// fixedClock returns a Clock that always returns the same time.
func fixedClock() Clock {
	t := time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC)
	return func() time.Time { return t }
}

// captureHandler records all records it receives for inspection.
type captureHandler struct {
	records []Record
}

func (h *captureHandler) Handle(rec Record) {
	h.records = append(h.records, rec)
}

// ---------------------------------------------------------------------------
// Logger tests
// ---------------------------------------------------------------------------

func TestLogger_EmitsAllLevels(t *testing.T) {
	h := &captureHandler{}
	log := New([]Handler{h}, WithClock(fixedClock()))

	log.Debug("debug msg")
	log.Info("info msg")
	log.Warn("warn msg")
	log.Error("error msg")

	if len(h.records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(h.records))
	}

	levels := []domain.Level{domain.LevelDebug, domain.LevelInfo, domain.LevelWarn, domain.LevelError}
	msgs := []string{"debug msg", "info msg", "warn msg", "error msg"}
	for i, rec := range h.records {
		if rec.Level != levels[i] {
			t.Errorf("record %d: expected level %d, got %d", i, levels[i], rec.Level)
		}
		if rec.Message != msgs[i] {
			t.Errorf("record %d: expected message %q, got %q", i, msgs[i], rec.Message)
		}
	}
}

func TestLogger_RecordCarriesTimestamp(t *testing.T) {
	h := &captureHandler{}
	clock := fixedClock()
	log := New([]Handler{h}, WithClock(clock))

	log.Info("test")

	if h.records[0].Time != clock() {
		t.Errorf("expected timestamp %v, got %v", clock(), h.records[0].Time)
	}
}

func TestLogger_WithFields(t *testing.T) {
	h := &captureHandler{}
	log := New([]Handler{h}, WithClock(fixedClock()))

	child := log.With(domain.F("key1", "val1"))
	child.Info("msg", domain.F("key2", "val2"))

	if len(h.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(h.records))
	}
	rec := h.records[0]
	if len(rec.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(rec.Fields))
	}
	if rec.Fields[0].Key != "key1" || rec.Fields[0].Value != "val1" {
		t.Errorf("field 0: got %v", rec.Fields[0])
	}
	if rec.Fields[1].Key != "key2" || rec.Fields[1].Value != "val2" {
		t.Errorf("field 1: got %v", rec.Fields[1])
	}
}

func TestLogger_Component(t *testing.T) {
	h := &captureHandler{}
	log := New([]Handler{h}, WithClock(fixedClock()))

	child := log.Component("feedparser")
	child.Info("parsing started")

	if h.records[0].Component != "feedparser" {
		t.Errorf("expected component %q, got %q", "feedparser", h.records[0].Component)
	}
}

func TestLogger_ComponentWithFields(t *testing.T) {
	h := &captureHandler{}
	log := New([]Handler{h}, WithClock(fixedClock()))

	child := log.Component("scheduler").(*Logger)
	grandchild := child.With(domain.F("worker", 3))
	grandchild.Info("job started")

	rec := h.records[0]
	if rec.Component != "scheduler" {
		t.Errorf("expected component %q, got %q", "scheduler", rec.Component)
	}
	if len(rec.Fields) != 1 || rec.Fields[0].Key != "worker" {
		t.Errorf("expected field worker, got %v", rec.Fields)
	}
}

func TestLogger_MultipleHandlers(t *testing.T) {
	h1 := &captureHandler{}
	h2 := &captureHandler{}
	log := New([]Handler{h1, h2}, WithClock(fixedClock()))

	log.Info("broadcast")

	if len(h1.records) != 1 {
		t.Errorf("handler 1: expected 1 record, got %d", len(h1.records))
	}
	if len(h2.records) != 1 {
		t.Errorf("handler 2: expected 1 record, got %d", len(h2.records))
	}
}

// ---------------------------------------------------------------------------
// Level / Verbosity tests
// ---------------------------------------------------------------------------

func TestShouldDisplay_Quiet(t *testing.T) {
	tests := []struct {
		level domain.Level
		want  bool
	}{
		{domain.LevelDebug, false},
		{domain.LevelInfo, false},
		{domain.LevelWarn, true},
		{domain.LevelError, true},
	}
	for _, tt := range tests {
		got := ShouldDisplay(tt.level, domain.VerbosityQuiet)
		if got != tt.want {
			t.Errorf("ShouldDisplay(%d, Quiet) = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestShouldDisplay_Normal(t *testing.T) {
	tests := []struct {
		level domain.Level
		want  bool
	}{
		{domain.LevelDebug, false},
		{domain.LevelInfo, true},
		{domain.LevelWarn, true},
		{domain.LevelError, true},
	}
	for _, tt := range tests {
		got := ShouldDisplay(tt.level, domain.VerbosityNormal)
		if got != tt.want {
			t.Errorf("ShouldDisplay(%d, Normal) = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestShouldDisplay_Verbose(t *testing.T) {
	tests := []struct {
		level domain.Level
		want  bool
	}{
		{domain.LevelDebug, true},
		{domain.LevelInfo, true},
		{domain.LevelWarn, true},
		{domain.LevelError, true},
	}
	for _, tt := range tests {
		got := ShouldDisplay(tt.level, domain.VerbosityVerbose)
		if got != tt.want {
			t.Errorf("ShouldDisplay(%d, Verbose) = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level domain.Level
		want  string
	}{
		{domain.LevelDebug, "DEBUG"},
		{domain.LevelInfo, "INFO"},
		{domain.LevelWarn, "WARN"},
		{domain.LevelError, "ERROR"},
	}
	for _, tt := range tests {
		got := LevelString(tt.level)
		if got != tt.want {
			t.Errorf("LevelString(%d) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Handler tests
// ---------------------------------------------------------------------------

func TestPlainHandler_Format(t *testing.T) {
	var buf bytes.Buffer
	h := NewPlainHandler(&buf, domain.VerbosityVerbose, nil)

	rec := Record{
		Time:      time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
		Level:     domain.LevelInfo,
		Component: "test",
		Message:   "hello world",
		Fields:    []domain.Field{{Key: "url", Value: "http://example.com"}},
	}
	h.Handle(rec)

	line := buf.String()
	if !strings.Contains(line, "2024-06-15 10:30:45") {
		t.Errorf("expected timestamp in output, got: %s", line)
	}
	if !strings.Contains(line, "[INFO]") {
		t.Errorf("expected [INFO] label, got: %s", line)
	}
	if !strings.Contains(line, "[test]") {
		t.Errorf("expected [test] component, got: %s", line)
	}
	if !strings.Contains(line, "hello world") {
		t.Errorf("expected message, got: %s", line)
	}
	if !strings.Contains(line, "url=http://example.com") {
		t.Errorf("expected field, got: %s", line)
	}
}

func TestPlainHandler_VerbosityFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := NewPlainHandler(&buf, domain.VerbosityNormal, nil)

	rec := Record{
		Time:    time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
		Level:   domain.LevelDebug,
		Message: "should be filtered",
	}
	h.Handle(rec)

	if buf.Len() != 0 {
		t.Errorf("expected debug record to be filtered, got: %s", buf.String())
	}
}

func TestTTYHandler_Format(t *testing.T) {
	var buf bytes.Buffer
	h := NewTTYHandler(&buf, domain.VerbosityVerbose, nil)

	rec := Record{
		Time:      time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
		Level:     domain.LevelWarn,
		Component: "proxy",
		Message:   "connection slow",
	}
	h.Handle(rec)

	line := buf.String()
	if !strings.Contains(line, "10:30:45") {
		t.Errorf("expected time in output, got: %s", line)
	}
	if !strings.Contains(line, "WARN") {
		t.Errorf("expected WARN in output, got: %s", line)
	}
	if !strings.Contains(line, "proxy") {
		t.Errorf("expected component in output, got: %s", line)
	}
	if !strings.Contains(line, "connection slow") {
		t.Errorf("expected message in output, got: %s", line)
	}
	// TTY handler should include ANSI escape codes.
	if !strings.Contains(line, "\033[") {
		t.Errorf("expected ANSI codes in TTY output, got: %s", line)
	}
}

func TestTTYHandler_VerbosityFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := NewTTYHandler(&buf, domain.VerbosityQuiet, nil)

	rec := Record{
		Time:    time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
		Level:   domain.LevelInfo,
		Message: "should be filtered",
	}
	h.Handle(rec)

	if buf.Len() != 0 {
		t.Errorf("expected info record to be filtered in quiet mode, got: %s", buf.String())
	}
}

func TestFileHandler_IgnoresVerbosity(t *testing.T) {
	var buf bytes.Buffer
	h := NewFileHandler(&buf, nil)

	// File handler should write ALL levels regardless of any verbosity setting.
	levels := []domain.Level{domain.LevelDebug, domain.LevelInfo, domain.LevelWarn, domain.LevelError}
	for _, lvl := range levels {
		rec := Record{
			Time:    time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
			Level:   lvl,
			Message: "test",
		}
		h.Handle(rec)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (all levels), got %d: %v", len(lines), lines)
	}
}

func TestFileHandler_Format(t *testing.T) {
	var buf bytes.Buffer
	h := NewFileHandler(&buf, nil)

	rec := Record{
		Time:      time.Date(2024, 6, 15, 10, 30, 45, 123000000, time.UTC),
		Level:     domain.LevelError,
		Component: "downloader",
		Message:   "ffmpeg failed",
		Fields:    []domain.Field{{Key: "exit_code", Value: 1}},
	}
	h.Handle(rec)

	line := buf.String()
	if !strings.Contains(line, "2024-06-15 10:30:45.123") {
		t.Errorf("expected full timestamp, got: %s", line)
	}
	if !strings.Contains(line, "ERROR") {
		t.Errorf("expected ERROR label, got: %s", line)
	}
	if !strings.Contains(line, "[downloader]") {
		t.Errorf("expected component, got: %s", line)
	}
	if !strings.Contains(line, "ffmpeg failed") {
		t.Errorf("expected message, got: %s", line)
	}
	if !strings.Contains(line, "exit_code=1") {
		t.Errorf("expected field, got: %s", line)
	}
	// File handler should NOT contain ANSI codes.
	if strings.Contains(line, "\033[") {
		t.Errorf("file handler should not contain ANSI codes, got: %s", line)
	}
}

// ---------------------------------------------------------------------------
// Coordinator tests
// ---------------------------------------------------------------------------

func TestCoordinator_WriteLog(t *testing.T) {
	var buf bytes.Buffer
	coord := NewCoordinator(&buf)

	coord.WriteLog("hello\n")

	if buf.String() != "hello\n" {
		t.Errorf("expected %q, got %q", "hello\n", buf.String())
	}
}

func TestCoordinator_WriteLogWithRedraw(t *testing.T) {
	var buf bytes.Buffer
	coord := NewCoordinator(&buf)

	redrawn := false
	coord.SetRedraw(func() {
		redrawn = true
	})

	coord.WriteLog("log line\n")

	if !strings.Contains(buf.String(), "log line\n") {
		t.Errorf("expected log line in output, got: %s", buf.String())
	}
	if !redrawn {
		t.Error("expected redraw callback to be called")
	}
}

func TestCoordinator_WriteProgress(t *testing.T) {
	var buf bytes.Buffer
	coord := NewCoordinator(&buf)

	called := false
	coord.WriteProgress(func() {
		called = true
	})

	if !called {
		t.Error("expected progress callback to be called")
	}
}
