package logx

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// genLevel generates a valid domain.Level (Debug, Info, Warn, Error).
func genLevel() *rapid.Generator[domain.Level] {
	return rapid.Map(rapid.IntRange(0, 3), func(i int) domain.Level {
		return domain.Level(i)
	})
}

// genVerbosity generates a valid domain.Verbosity (Quiet, Normal, Verbose).
func genVerbosity() *rapid.Generator[domain.Verbosity] {
	return rapid.Map(rapid.IntRange(0, 2), func(i int) domain.Verbosity {
		return domain.Verbosity(i)
	})
}

// genComponent generates a non-empty component name.
func genComponent() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_]{0,15}`)
}

// genMessage generates a non-empty log message.
func genMessage() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z][A-Za-z0-9 ]{0,30}`)
}

// genField generates a structured Field with a non-empty key.
func genField() *rapid.Generator[domain.Field] {
	return rapid.Custom[domain.Field](func(t *rapid.T) domain.Field {
		key := rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`).Draw(t, "key")
		value := rapid.OneOf(
			rapid.Map(rapid.Int(), func(i int) any { return i }),
			rapid.Map(rapid.String(), func(s string) any { return s }),
			rapid.Map(rapid.Bool(), func(b bool) any { return b }),
		).Draw(t, "value")
		return domain.Field{Key: key, Value: value}
	})
}

// genFields generates a slice of 0-5 fields.
func genFields() *rapid.Generator[[]domain.Field] {
	return rapid.SliceOfN(genField(), 0, 5)
}

// ---------------------------------------------------------------------------
// Property 35: Records always carry timestamp, level, component, and fields
// ---------------------------------------------------------------------------

// **Validates: Requirements 13.5**
//
// For any generated message, component, and fields, the emitted Record always
// carries a non-zero timestamp, the correct level, the component name, and all
// supplied fields.
func TestProperty35_RecordCarriesAllMetadata(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		level := genLevel().Draw(t, "level")
		component := genComponent().Draw(t, "component")
		msg := genMessage().Draw(t, "message")
		fields := genFields().Draw(t, "fields")

		h := &captureHandler{}
		fixedTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
		clock := func() time.Time { return fixedTime }
		logger := New([]Handler{h}, WithClock(clock))

		// Create a child logger with the component name.
		child := logger.Component(component).(*Logger)

		// Emit at the drawn level.
		switch level {
		case domain.LevelDebug:
			child.Debug(msg, fields...)
		case domain.LevelInfo:
			child.Info(msg, fields...)
		case domain.LevelWarn:
			child.Warn(msg, fields...)
		case domain.LevelError:
			child.Error(msg, fields...)
		}

		if len(h.records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(h.records))
		}
		rec := h.records[0]

		// Non-zero timestamp.
		if rec.Time.IsZero() {
			t.Fatal("record timestamp is zero")
		}
		if rec.Time != fixedTime {
			t.Fatalf("expected timestamp %v, got %v", fixedTime, rec.Time)
		}

		// Correct level.
		if rec.Level != level {
			t.Fatalf("expected level %d, got %d", level, rec.Level)
		}

		// Component name.
		if rec.Component != component {
			t.Fatalf("expected component %q, got %q", component, rec.Component)
		}

		// All supplied fields present.
		if len(rec.Fields) != len(fields) {
			t.Fatalf("expected %d fields, got %d", len(fields), len(rec.Fields))
		}
		for i, f := range fields {
			if rec.Fields[i].Key != f.Key {
				t.Fatalf("field %d: expected key %q, got %q", i, f.Key, rec.Fields[i].Key)
			}
			if rec.Fields[i].Value != f.Value {
				t.Fatalf("field %d: expected value %v, got %v", i, f.Value, rec.Fields[i].Value)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 36: File sink ignores verbosity
// ---------------------------------------------------------------------------

// **Validates: Requirements 13.7**
//
// For any level and any verbosity setting, the file handler always writes the
// record (never filters).
func TestProperty36_FileHandlerIgnoresVerbosity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		level := genLevel().Draw(t, "level")
		verbosity := genVerbosity().Draw(t, "verbosity")
		msg := genMessage().Draw(t, "message")

		var buf bytes.Buffer
		h := NewFileHandler(&buf, nil)

		rec := Record{
			Time:    time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
			Level:   level,
			Message: msg,
		}

		// Regardless of verbosity (which the file handler doesn't even accept),
		// the record should always be written.
		_ = verbosity // verbosity is drawn to show it doesn't matter
		h.Handle(rec)

		output := buf.String()
		if output == "" {
			t.Fatalf("file handler produced no output for level=%d verbosity=%d", level, verbosity)
		}
		if !strings.Contains(output, msg) {
			t.Fatalf("file handler output does not contain message %q: %s", msg, output)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 37: Verbosity threshold filters displayed levels
// ---------------------------------------------------------------------------

// **Validates: Requirements 14.2, 14.3, 14.4**
//
// For any level and verbosity combination, ShouldDisplay returns true iff
// level >= minLevel(verbosity). Specifically:
//   - quiet filters debug+info
//   - normal filters debug
//   - verbose filters nothing
func TestProperty37_VerbosityThresholdFilters(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		level := genLevel().Draw(t, "level")
		verbosity := genVerbosity().Draw(t, "verbosity")

		got := ShouldDisplay(level, verbosity)

		// Compute expected result based on the spec.
		var expected bool
		switch verbosity {
		case domain.VerbosityQuiet:
			// Only warn and error pass.
			expected = level >= domain.LevelWarn
		case domain.VerbosityNormal:
			// Info, warn, error pass (debug filtered).
			expected = level >= domain.LevelInfo
		case domain.VerbosityVerbose:
			// Everything passes.
			expected = true
		}

		if got != expected {
			t.Fatalf("ShouldDisplay(level=%d, verbosity=%d) = %v, want %v",
				level, verbosity, got, expected)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 38: TTY rendering uses a distinct color per severity
// ---------------------------------------------------------------------------

// **Validates: Requirements 14.5**
//
// For any level, the TTY handler output contains ANSI escape codes, and
// different levels produce different color prefixes.
func TestProperty38_TTYHandlerUsesDistinctColors(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		level := genLevel().Draw(t, "level")
		msg := genMessage().Draw(t, "message")

		var buf bytes.Buffer
		h := NewTTYHandler(&buf, domain.VerbosityVerbose, nil)

		rec := Record{
			Time:    time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
			Level:   level,
			Message: msg,
		}
		h.Handle(rec)

		output := buf.String()

		// Must contain ANSI escape codes.
		if !strings.Contains(output, "\033[") {
			t.Fatalf("TTY output for level=%d does not contain ANSI escape codes: %q", level, output)
		}

		// Verify that different levels produce different color codes.
		// We check that the level-specific color is present in the output.
		expectedColor := levelColor(level)
		if !strings.Contains(output, expectedColor) {
			t.Fatalf("TTY output for level=%d does not contain expected color %q: %q",
				level, expectedColor, output)
		}
	})

	// Additionally verify that all four levels produce distinct colors.
	colors := make(map[string]domain.Level)
	levels := []domain.Level{domain.LevelDebug, domain.LevelInfo, domain.LevelWarn, domain.LevelError}
	for _, l := range levels {
		c := levelColor(l)
		if prev, exists := colors[c]; exists {
			t.Fatalf("levels %d and %d share the same color %q", prev, l, c)
		}
		colors[c] = l
	}
}

// ---------------------------------------------------------------------------
// Property 39: Non-TTY rendering is plain-text labeled
// ---------------------------------------------------------------------------

// **Validates: Requirements 14.6**
//
// For any level, the plain handler output contains a bracketed severity label
// like [INFO] and does NOT contain ANSI escape codes.
func TestProperty39_PlainHandlerNoANSI(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		level := genLevel().Draw(t, "level")
		msg := genMessage().Draw(t, "message")

		var buf bytes.Buffer
		h := NewPlainHandler(&buf, domain.VerbosityVerbose, nil)

		rec := Record{
			Time:    time.Date(2024, 6, 15, 10, 30, 45, 0, time.UTC),
			Level:   level,
			Message: msg,
		}
		h.Handle(rec)

		output := buf.String()

		// Must contain a bracketed severity label.
		expectedLabel := "[" + LevelString(level) + "]"
		if !strings.Contains(output, expectedLabel) {
			t.Fatalf("plain output for level=%d does not contain label %q: %q",
				level, expectedLabel, output)
		}

		// Must NOT contain ANSI escape codes.
		if strings.Contains(output, "\033[") {
			t.Fatalf("plain output for level=%d contains ANSI escape codes: %q",
				level, output)
		}
	})
}
