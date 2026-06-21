// Package termx provides terminal detection, ANSI color helpers, and terminal
// width querying for the logging and progress display subsystems.
// (Req 10.8, 14.5, 14.6)
package termx

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// ANSI escape sequences for color rendering.
// These are standard ANSI codes supported on macOS, Linux, and Termux.
const (
	// Reset clears all ANSI formatting.
	Reset = "\033[0m"

	// Exported color constants used by logx and other packages.
	Cyan      = "\033[36m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Red       = "\033[31m"
	Blue      = "\033[34m"
	Magenta   = "\033[35m"
	Gray      = "\033[90m"
	White     = "\033[37m"
	Bold      = "\033[1m"
	BoldRed   = "\033[1;31m"
	BoldGreen = "\033[1;32m"
	BoldCyan  = "\033[1;36m"
)

// defaultWidth is used when terminal width cannot be detected.
const defaultWidth = 80

// IsTTY reports whether f is connected to a terminal.
func IsTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// ColorDebug wraps s in cyan ANSI escape codes (debug level color).
func ColorDebug(s string) string {
	return fmt.Sprintf("%s%s%s", Cyan, s, Reset)
}

// ColorInfo wraps s in green ANSI escape codes (info level color).
func ColorInfo(s string) string {
	return fmt.Sprintf("%s%s%s", Green, s, Reset)
}

// ColorWarn wraps s in yellow ANSI escape codes (warn level color).
func ColorWarn(s string) string {
	return fmt.Sprintf("%s%s%s", Yellow, s, Reset)
}

// ColorError wraps s in red ANSI escape codes (error level color).
func ColorError(s string) string {
	return fmt.Sprintf("%s%s%s", Red, s, Reset)
}

// TerminalWidth returns the width of the terminal connected to stdout.
// If detection fails (e.g., not a TTY), it returns 80.
func TerminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return defaultWidth
	}
	return w
}
