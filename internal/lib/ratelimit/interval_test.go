package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a deterministic clock for testing.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- c.now.Add(d)
	return ch
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestInterval_FirstCallNeverBlocks(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := NewInterval(clk, 1000) // 1s interval

	before := clk.Now()
	lim.Wait()
	after := clk.Now()

	if !before.Equal(after) {
		t.Errorf("first Wait() should not block; time advanced by %v", after.Sub(before))
	}
}

func TestInterval_SecondCallBlocksForRemainingInterval(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := NewInterval(clk, 1000) // 1s interval

	lim.Wait() // first call at t=0

	// Advance 400ms (less than the 1s interval)
	clk.advance(400 * time.Millisecond)

	before := clk.Now()
	lim.Wait() // should sleep for 600ms
	after := clk.Now()

	elapsed := after.Sub(before)
	expected := 600 * time.Millisecond
	if elapsed != expected {
		t.Errorf("expected sleep of %v, got %v", expected, elapsed)
	}
}

func TestInterval_NoBlockWhenIntervalElapsed(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := NewInterval(clk, 500) // 500ms interval

	lim.Wait() // first call

	// Advance more than the interval
	clk.advance(600 * time.Millisecond)

	before := clk.Now()
	lim.Wait()
	after := clk.Now()

	if !before.Equal(after) {
		t.Errorf("should not block when interval already elapsed; slept %v", after.Sub(before))
	}
}

func TestInterval_ZeroIntervalNeverBlocks(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := NewInterval(clk, 0)

	lim.Wait()
	before := clk.Now()
	lim.Wait()
	after := clk.Now()

	if !before.Equal(after) {
		t.Errorf("zero interval should never block; slept %v", after.Sub(before))
	}
}

func TestInterval_ClampsNegativeToZero(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := NewInterval(clk, -100)

	lim.Wait()
	before := clk.Now()
	lim.Wait()
	after := clk.Now()

	if !before.Equal(after) {
		t.Errorf("negative interval should be clamped to 0; slept %v", after.Sub(before))
	}
}

func TestInterval_ClampsAbove60000(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := NewInterval(clk, 99999)

	lim.Wait() // first call

	before := clk.Now()
	lim.Wait() // should sleep for 60000ms (clamped)
	after := clk.Now()

	expected := 60000 * time.Millisecond
	elapsed := after.Sub(before)
	if elapsed != expected {
		t.Errorf("expected clamped sleep of %v, got %v", expected, elapsed)
	}
}

func TestInterval_SuccessiveCallsMaintainInterval(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lim := NewInterval(clk, 200) // 200ms interval

	// First call
	lim.Wait()
	t1 := clk.Now()

	// Second call immediately
	lim.Wait()
	t2 := clk.Now()

	// Third call immediately
	lim.Wait()
	t3 := clk.Now()

	interval := 200 * time.Millisecond
	if t2.Sub(t1) < interval {
		t.Errorf("gap between call 1 and 2: %v < %v", t2.Sub(t1), interval)
	}
	if t3.Sub(t2) < interval {
		t.Errorf("gap between call 2 and 3: %v < %v", t3.Sub(t2), interval)
	}
}
