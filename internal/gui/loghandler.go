package gui

import (
	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/logx"
)

// uiLogHandler is a logx.Handler that forwards engine log records to a job's log
// buffer (and thus to the UI). It honours the configured verbosity so the UI log
// view mirrors what the CLI would print.
type uiLogHandler struct {
	mgr       *JobManager
	job       *Job
	verbosity domain.Verbosity
}

func (h *uiLogHandler) Handle(rec logx.Record) {
	if !logx.ShouldDisplay(rec.Level, h.verbosity) {
		return
	}
	var fields map[string]any
	if len(rec.Fields) > 0 {
		fields = make(map[string]any, len(rec.Fields))
		for _, f := range rec.Fields {
			fields[f.Key] = f.Value
		}
	}
	h.job.addLog(LogEntry{
		Time:      rec.Time,
		Level:     logx.LevelString(rec.Level),
		Component: rec.Component,
		Message:   rec.Message,
		Fields:    fields,
	})
}

// newUILogger builds a domain.Logger that streams records into the job's UI log.
func newUILogger(mgr *JobManager, job *Job, verbosity domain.Verbosity) domain.Logger {
	return logx.New([]logx.Handler{&uiLogHandler{mgr: mgr, job: job, verbosity: verbosity}})
}

// captureLogHandler collects records into a slice for one-shot operations
// (preview, doctor) where there is no long-lived job to stream into.
type captureLogHandler struct {
	verbosity domain.Verbosity
	entries   []LogEntry
}

func (h *captureLogHandler) Handle(rec logx.Record) {
	if !logx.ShouldDisplay(rec.Level, h.verbosity) {
		return
	}
	var fields map[string]any
	if len(rec.Fields) > 0 {
		fields = make(map[string]any, len(rec.Fields))
		for _, f := range rec.Fields {
			fields[f.Key] = f.Value
		}
	}
	h.entries = append(h.entries, LogEntry{
		Time:      rec.Time,
		Level:     logx.LevelString(rec.Level),
		Component: rec.Component,
		Message:   rec.Message,
		Fields:    fields,
	})
}

func newCaptureLogger(verbosity domain.Verbosity) (domain.Logger, *captureLogHandler) {
	h := &captureLogHandler{verbosity: verbosity}
	return logx.New([]logx.Handler{h}), h
}
