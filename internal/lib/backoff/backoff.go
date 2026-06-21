// Package backoff provides exponential backoff delay computation with jitter.
package backoff

import (
	"math/rand"
	"time"
)

const (
	baseDelay = 1 * time.Second
	maxDelay  = 60 * time.Second
	maxJitter = 1 * time.Second
)

// Delay computes the backoff delay for the given attempt number.
// The base delay is min(1s * 2^attempt, 60s), with random jitter ∈ [0, 1s)
// added on top. The rng parameter allows injecting a deterministic rand source
// for testing.
func Delay(attempt int, rng *rand.Rand) time.Duration {
	base := baseDelay << uint(attempt) // 1s * 2^attempt
	if base > maxDelay || base <= 0 {  // overflow or exceeds cap
		base = maxDelay
	}

	jitter := time.Duration(rng.Int63n(int64(maxJitter)))

	return base + jitter
}
