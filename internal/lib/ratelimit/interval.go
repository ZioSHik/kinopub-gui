// Package ratelimit provides a minimum-interval rate limiter that ensures
// successive calls are separated by at least a configured duration.
package ratelimit

import (
	"sync"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Interval enforces a minimum time gap between successive Wait() calls.
// It is safe for concurrent use.
type Interval struct {
	mu       sync.Mutex
	clock    domain.Clock
	interval time.Duration
	last     time.Time
}

// NewInterval creates a rate limiter that enforces the given minimum interval
// between successive calls to Wait. The interval must be in [0, 60000] ms;
// values outside this range are clamped.
func NewInterval(clock domain.Clock, intervalMS int) *Interval {
	if intervalMS < 0 {
		intervalMS = 0
	}
	if intervalMS > 60000 {
		intervalMS = 60000
	}

	return &Interval{
		clock:    clock,
		interval: time.Duration(intervalMS) * time.Millisecond,
	}
}

// Wait blocks until at least the configured minimum interval has elapsed since
// the last call to Wait. The first call never blocks. Blocking is performed
// via the injected Clock.Sleep so that tests can control time deterministically.
func (i *Interval) Wait() {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.clock.Now()

	if !i.last.IsZero() {
		elapsed := now.Sub(i.last)
		if elapsed < i.interval {
			sleep := i.interval - elapsed
			i.mu.Unlock()
			i.clock.Sleep(sleep)
			i.mu.Lock()
			// Update now after sleeping.
			now = i.clock.Now()
		}
	}

	i.last = now
}
