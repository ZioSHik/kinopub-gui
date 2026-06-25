package gui

import (
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestCaptureLogger_FiltersByVerbosity(t *testing.T) {
	// Quiet verbosity shows only warn/error; Info must be dropped.
	logger, cap := newCaptureLogger(domain.VerbosityQuiet)
	logger.Info("an info line")
	logger.Warn("a warning", domain.F("code", 7))

	if len(cap.entries) != 1 {
		t.Fatalf("quiet should keep only warn/error, got %d entries", len(cap.entries))
	}
	e := cap.entries[0]
	if e.Message != "a warning" {
		t.Errorf("kept the wrong entry: %+v", e)
	}
	if e.Level == "" {
		t.Error("level string should be populated")
	}
	if e.Fields == nil || e.Fields["code"] != 7 {
		t.Errorf("fields not mapped: %+v", e.Fields)
	}
}

func TestCaptureLogger_VerboseKeepsInfo(t *testing.T) {
	logger, cap := newCaptureLogger(domain.VerbosityVerbose)
	logger.Info("info line")
	if len(cap.entries) != 1 {
		t.Fatalf("verbose should keep info, got %d", len(cap.entries))
	}
	if cap.entries[0].Fields != nil {
		t.Errorf("no fields → Fields should be nil, got %+v", cap.entries[0].Fields)
	}
}

func TestUILogger_StreamsIntoJob(t *testing.T) {
	m := newJobManager(newHub())
	j := newJob("j", "u", domain.RunConfig{})
	m.add(j)
	logger := newUILogger(m, j, domain.VerbosityNormal)

	logger.Info("hello", domain.F("k", "v"))
	logger.Warn("careful")

	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.logs) != 2 {
		t.Fatalf("expected 2 log entries on the job, got %d", len(j.logs))
	}
	if j.logs[0].Message != "hello" || j.logs[0].Fields["k"] != "v" {
		t.Errorf("first log entry wrong: %+v", j.logs[0])
	}
}
