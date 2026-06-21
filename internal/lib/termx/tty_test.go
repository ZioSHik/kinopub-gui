package termx

import (
	"os"
	"strings"
	"testing"
)

func TestIsTTY_RegularFile(t *testing.T) {
	f, err := os.CreateTemp("", "termx-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	if IsTTY(f) {
		t.Error("expected regular file to not be a TTY")
	}
}

func TestColorDebug(t *testing.T) {
	got := ColorDebug("hello")
	if !strings.Contains(got, "hello") {
		t.Error("ColorDebug should contain the original string")
	}
	if !strings.HasPrefix(got, Cyan) {
		t.Error("ColorDebug should start with cyan escape")
	}
	if !strings.HasSuffix(got, Reset) {
		t.Error("ColorDebug should end with reset escape")
	}
}

func TestColorInfo(t *testing.T) {
	got := ColorInfo("hello")
	if !strings.HasPrefix(got, Green) {
		t.Error("ColorInfo should start with green escape")
	}
	if !strings.HasSuffix(got, Reset) {
		t.Error("ColorInfo should end with reset escape")
	}
}

func TestColorWarn(t *testing.T) {
	got := ColorWarn("hello")
	if !strings.HasPrefix(got, Yellow) {
		t.Error("ColorWarn should start with yellow escape")
	}
	if !strings.HasSuffix(got, Reset) {
		t.Error("ColorWarn should end with reset escape")
	}
}

func TestColorError(t *testing.T) {
	got := ColorError("hello")
	if !strings.HasPrefix(got, Red) {
		t.Error("ColorError should start with red escape")
	}
	if !strings.HasSuffix(got, Reset) {
		t.Error("ColorError should end with reset escape")
	}
}

func TestTerminalWidth_ReturnsPositive(t *testing.T) {
	w := TerminalWidth()
	if w <= 0 {
		t.Errorf("TerminalWidth() = %d, want > 0", w)
	}
}

func TestTerminalWidth_DefaultWhenNotTTY(t *testing.T) {
	// In CI or piped test environments, stdout is typically not a TTY,
	// so TerminalWidth should return the default of 80.
	// When running in a real terminal it returns the actual width.
	// Either way, the result must be positive.
	w := TerminalWidth()
	if w <= 0 {
		t.Errorf("TerminalWidth() = %d, want > 0", w)
	}
}

func TestColorFunctions_DistinctColors(t *testing.T) {
	d := ColorDebug("x")
	i := ColorInfo("x")
	w := ColorWarn("x")
	e := ColorError("x")

	// Each level should produce a different colored output.
	results := map[string]string{
		"debug": d,
		"info":  i,
		"warn":  w,
		"error": e,
	}

	seen := make(map[string]string)
	for level, val := range results {
		for prevLevel, prevVal := range seen {
			if val == prevVal {
				t.Errorf("Color functions should produce distinct outputs, but %s == %s", level, prevLevel)
			}
		}
		seen[level] = val
	}
}

func TestReset_IsANSIEscape(t *testing.T) {
	if Reset != "\033[0m" {
		t.Errorf("Reset = %q, want %q", Reset, "\033[0m")
	}
}
