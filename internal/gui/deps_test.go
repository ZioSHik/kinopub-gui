package gui

import (
	"strings"
	"testing"
)

func TestTailWriterKeepsTail(t *testing.T) {
	w := &tailWriter{max: 8}
	if _, err := w.Write([]byte("123456")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("7890")); err != nil { // total 10 → keep last 8
		t.Fatal(err)
	}
	w.mu.Lock()
	got := string(w.buf)
	w.mu.Unlock()
	if got != "34567890" {
		t.Errorf("tail buffer = %q, want %q", got, "34567890")
	}
}

func TestTailWriterReportsFullWriteLength(t *testing.T) {
	w := &tailWriter{max: 4}
	n, err := w.Write([]byte("longinput"))
	if err != nil {
		t.Fatal(err)
	}
	// Write reports the full input length even though only the tail is kept.
	if n != len("longinput") {
		t.Errorf("Write returned %d, want %d", n, len("longinput"))
	}
}

func TestTailWriterLastLines(t *testing.T) {
	w := &tailWriter{max: 4096}
	// Mixed newlines and carriage returns; empty lines are dropped.
	w.Write([]byte("frame 1\r"))
	w.Write([]byte("frame 2\r"))
	w.Write([]byte("\nError: stream not found\n\n"))
	got := w.lastLines(2)
	// lastLines returns the last 2 non-empty lines joined by " | ".
	if !strings.Contains(got, "Error: stream not found") {
		t.Errorf("lastLines missing the error: %q", got)
	}
	if strings.Count(got, " | ") != 1 {
		t.Errorf("expected exactly 2 lines joined by ' | ', got %q", got)
	}
}

func TestTailWriterLastLinesEmpty(t *testing.T) {
	w := &tailWriter{max: 16}
	if got := w.lastLines(5); got != "" {
		t.Errorf("empty writer lastLines = %q, want empty", got)
	}
	w.Write([]byte("\n\n  \n"))
	if got := w.lastLines(5); got != "" {
		t.Errorf("whitespace-only lastLines = %q, want empty", got)
	}
}

func TestRealClock(t *testing.T) {
	c := realClock{}
	a := c.Now()
	b := c.Now()
	if b.Before(a) {
		t.Error("clock went backwards")
	}
	// After returns a channel; Sleep(0) is a no-op — exercise both without waiting.
	if ch := c.After(0); ch == nil {
		t.Error("After should return a channel")
	}
	c.Sleep(0)
}
