// Package logx implements a custom structured, leveled logging subsystem
// (Req 13, 14). It provides colored TTY output, plain-text non-TTY output,
// file logging at all levels, and verbosity filtering.
package logx

import "github.com/niazlv/kinopub-downloader/internal/domain"

// Re-export domain levels for convenience within the package.
const (
	LevelDebug = domain.LevelDebug
	LevelInfo  = domain.LevelInfo
	LevelWarn  = domain.LevelWarn
	LevelError = domain.LevelError
)

// LevelString returns a fixed-width uppercase label for the given level.
func LevelString(l domain.Level) string {
	switch l {
	case domain.LevelDebug:
		return "DEBUG"
	case domain.LevelInfo:
		return "INFO"
	case domain.LevelWarn:
		return "WARN"
	case domain.LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ShouldDisplay returns true if a record at the given level should be shown
// on the console given the configured verbosity.
//
// Verbosity filtering (Req 14.2-14.4):
//   - quiet  → warn + error only
//   - normal → info + warn + error
//   - verbose → debug + info + warn + error
//
// This filter is applied only to console handlers, NOT to the file handler
// (Req 13.7).
func ShouldDisplay(level domain.Level, verbosity domain.Verbosity) bool {
	minLevel := minLevelForVerbosity(verbosity)
	return level >= minLevel
}

// minLevelForVerbosity returns the minimum level that should be displayed
// for the given verbosity setting.
func minLevelForVerbosity(v domain.Verbosity) domain.Level {
	switch v {
	case domain.VerbosityQuiet:
		return domain.LevelWarn
	case domain.VerbosityNormal:
		return domain.LevelInfo
	case domain.VerbosityVerbose:
		return domain.LevelDebug
	default:
		return domain.LevelInfo
	}
}
