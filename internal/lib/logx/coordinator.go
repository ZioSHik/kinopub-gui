package logx

import (
	"fmt"
	"io"
	"sync"
)

// Coordinator provides a shared TTY line-discipline mutex that serializes
// log writes with progress display updates (Req 14.7). This ensures that
// log lines never corrupt the live progress display.
//
// The progress display owns a region at the bottom of the terminal. When a
// log line needs to be written, the coordinator:
//  1. Acquires the mutex
//  2. Clears the progress region (if active)
//  3. Writes the log line
//  4. Redraws the progress region (if active)
//  5. Releases the mutex
//
// The progress reporter uses the same mutex when updating its display.
type Coordinator struct {
	mu     sync.Mutex
	w      io.Writer
	redraw func() // callback to redraw progress display; nil if no live display
	clear  func() // callback to clear progress display before a log line
}

// NewCoordinator creates a Coordinator that writes to w.
func NewCoordinator(w io.Writer) *Coordinator {
	return &Coordinator{w: w}
}

// SetRedraw registers a callback that the coordinator calls after writing a
// log line to restore the progress display. The progress reporter sets this
// when it starts and clears it when it stops.
func (c *Coordinator) SetRedraw(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.redraw = fn
}

// SetClear registers a callback that the coordinator calls before writing a
// log line to erase the progress display. The progress reporter sets this
// when it starts and clears it when it stops.
func (c *Coordinator) SetClear(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clear = fn
}

// WriteLog writes a log line while coordinating with the progress display.
// If a progress display is active, it is temporarily cleared and then redrawn
// after the log line is written.
func (c *Coordinator) WriteLog(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.redraw != nil {
		// Clear the progress region, write the log line, then redraw progress.
		if c.clear != nil {
			c.clear()
		}
		fmt.Fprint(c.w, line)
		c.redraw()
	} else {
		fmt.Fprint(c.w, line)
	}
}

// WriteProgress acquires the mutex and calls fn, which should render the
// progress display. This ensures progress updates don't interleave with log
// writes.
func (c *Coordinator) WriteProgress(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn()
}

// Lock acquires the coordinator mutex directly. Use sparingly — prefer
// WriteLog and WriteProgress for structured access.
func (c *Coordinator) Lock() {
	c.mu.Lock()
}

// Unlock releases the coordinator mutex.
func (c *Coordinator) Unlock() {
	c.mu.Unlock()
}
