package logx

import (
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Record represents a single log entry with all contextual information
// (Req 13.5: timestamp, level, component, message, ordered fields).
type Record struct {
	Time      time.Time
	Level     domain.Level
	Component string
	Message   string
	Fields    []domain.Field
}

// Handler is the interface for log output sinks. Each handler receives every
// record and decides whether/how to render it.
type Handler interface {
	Handle(rec Record)
}

// Clock abstracts time for testability.
type Clock func() time.Time

// Logger is the concrete implementation of domain.Logger. It dispatches
// records to multiple handlers and supports child loggers with additional
// fields or a component name (Req 13.5).
type Logger struct {
	handlers  []Handler
	component string
	fields    []domain.Field
	clock     Clock
}

// Option configures a Logger at construction time.
type Option func(*Logger)

// WithClock sets a custom clock for timestamp generation (useful in tests).
func WithClock(c Clock) Option {
	return func(l *Logger) {
		l.clock = c
	}
}

// New creates a new Logger that dispatches records to the given handlers.
func New(handlers []Handler, opts ...Option) *Logger {
	l := &Logger{
		handlers: handlers,
		clock:    time.Now,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Debug emits a debug-level record.
func (l *Logger) Debug(msg string, fields ...domain.Field) {
	l.emit(domain.LevelDebug, msg, fields)
}

// Info emits an info-level record.
func (l *Logger) Info(msg string, fields ...domain.Field) {
	l.emit(domain.LevelInfo, msg, fields)
}

// Warn emits a warn-level record.
func (l *Logger) Warn(msg string, fields ...domain.Field) {
	l.emit(domain.LevelWarn, msg, fields)
}

// Error emits an error-level record.
func (l *Logger) Error(msg string, fields ...domain.Field) {
	l.emit(domain.LevelError, msg, fields)
}

// With returns a child logger that attaches the given fields to every
// subsequent record (Req 13.5).
func (l *Logger) With(fields ...domain.Field) domain.Logger {
	merged := make([]domain.Field, 0, len(l.fields)+len(fields))
	merged = append(merged, l.fields...)
	merged = append(merged, fields...)
	return &Logger{
		handlers:  l.handlers,
		component: l.component,
		fields:    merged,
		clock:     l.clock,
	}
}

// Component returns a child logger tagged with a component name (Req 13.5).
func (l *Logger) Component(name string) domain.Logger {
	return &Logger{
		handlers:  l.handlers,
		component: name,
		fields:    l.fields,
		clock:     l.clock,
	}
}

// emit constructs a Record and dispatches it to all handlers.
func (l *Logger) emit(level domain.Level, msg string, extra []domain.Field) {
	rec := Record{
		Time:      l.clock(),
		Level:     level,
		Component: l.component,
		Message:   msg,
	}
	// Merge logger-level fields with call-site fields.
	if len(l.fields) > 0 || len(extra) > 0 {
		all := make([]domain.Field, 0, len(l.fields)+len(extra))
		all = append(all, l.fields...)
		all = append(all, extra...)
		rec.Fields = all
	}
	for _, h := range l.handlers {
		h.Handle(rec)
	}
}

// Verify that *Logger satisfies domain.Logger at compile time.
var _ domain.Logger = (*Logger)(nil)
