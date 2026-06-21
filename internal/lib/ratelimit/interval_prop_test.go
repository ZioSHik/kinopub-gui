package ratelimit

import (
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 4.5**

// propClock is a fake clock for property testing that tracks time advances
// from Sleep calls and allows arbitrary external advances.
type propClock struct {
	mu  sync.Mutex
	now time.Time
}

func newPropClock() *propClock {
	return &propClock{now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *propClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *propClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *propClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- c.now.Add(d)
	return ch
}

func (c *propClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestProperty13_MinimumInterRequestInterval verifies that for any configured
// interval in [0, 60000] ms and any sequence of Wait() calls (with arbitrary
// time advances between them on a fake clock), the effective time between
// successive Wait() completions is always ≥ the configured interval.
//
// **Validates: Requirements 4.5**
func TestProperty13_MinimumInterRequestInterval(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random interval in [0, 60000] ms.
		intervalMS := rapid.IntRange(0, 60000).Draw(t, "intervalMS")
		interval := time.Duration(intervalMS) * time.Millisecond

		// Generate a sequence of 2-10 Wait() calls.
		numCalls := rapid.IntRange(2, 10).Draw(t, "numCalls")

		// Generate arbitrary time advances between calls (0 to 120s each).
		advances := make([]time.Duration, numCalls-1)
		for i := range advances {
			advanceMS := rapid.IntRange(0, 120000).Draw(t, "advanceMS")
			advances[i] = time.Duration(advanceMS) * time.Millisecond
		}

		clk := newPropClock()
		lim := NewInterval(clk, intervalMS)

		// Record the completion time of each Wait() call.
		completionTimes := make([]time.Time, numCalls)

		// First call.
		lim.Wait()
		completionTimes[0] = clk.Now()

		// Subsequent calls with arbitrary advances between them.
		for i := 1; i < numCalls; i++ {
			// Advance the clock by an arbitrary amount before the next Wait().
			clk.advance(advances[i-1])

			lim.Wait()
			completionTimes[i] = clk.Now()
		}

		// Verify: every pair of successive completions is separated by ≥ interval.
		for i := 1; i < numCalls; i++ {
			gap := completionTimes[i].Sub(completionTimes[i-1])
			if gap < interval {
				t.Fatalf(
					"gap between Wait() call %d and %d is %v, less than configured interval %v (intervalMS=%d)",
					i-1, i, gap, interval, intervalMS,
				)
			}
		}
	})
}
