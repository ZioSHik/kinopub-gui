package backoff

import (
	"math/rand"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 5.4**

// Property 16: Backoff delays are bounded and monotonic
//
// 1. For any attempt n ∈ [0, 100], the delay is ≥ base and < base + 1s
//    where base = min(1s*2^n, 60s).
// 2. For any sequence of attempts 0..n, the base component is monotonically
//    non-decreasing.
// 3. The total delay is always in [base, base+1s) where base = min(1s*2^attempt, 60s).

func expectedBase(attempt int) time.Duration {
	base := time.Second << uint(attempt)
	if base > 60*time.Second || base <= 0 {
		base = 60 * time.Second
	}
	return base
}

func TestProperty16_DelayBoundedForAnyAttempt(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(0, 100).Draw(t, "attempt")
		seed := rapid.Int64().Draw(t, "seed")
		rng := rand.New(rand.NewSource(seed))

		d := Delay(attempt, rng)
		base := expectedBase(attempt)

		if d < base {
			t.Fatalf("Delay(%d) = %v, want >= base %v", attempt, d, base)
		}
		if d >= base+time.Second {
			t.Fatalf("Delay(%d) = %v, want < base+1s = %v", attempt, d, base+time.Second)
		}
	})
}

func TestProperty16_BaseMonotonicallyNonDecreasing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 100).Draw(t, "maxAttempt")
		seed := rapid.Int64().Draw(t, "seed")
		rng := rand.New(rand.NewSource(seed))

		var prevBase time.Duration
		for attempt := 0; attempt <= n; attempt++ {
			d := Delay(attempt, rng)
			base := expectedBase(attempt)

			// Verify the actual delay is consistent with the expected base
			if d < base || d >= base+time.Second {
				t.Fatalf("attempt %d: Delay = %v not in [%v, %v)", attempt, d, base, base+time.Second)
			}

			// Verify monotonicity of the base component
			if base < prevBase {
				t.Fatalf("attempt %d: base %v < previous base %v (not monotonically non-decreasing)", attempt, base, prevBase)
			}
			prevBase = base
		}
	})
}

func TestProperty16_TotalDelayInBaseToBasePlusJitter(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(0, 100).Draw(t, "attempt")
		seed := rapid.Int64().Draw(t, "seed")
		rng := rand.New(rand.NewSource(seed))

		d := Delay(attempt, rng)
		base := expectedBase(attempt)

		// Total delay must be in [base, base + 1s)
		if d < base {
			t.Fatalf("Delay(%d) = %v < base %v", attempt, d, base)
		}
		if d >= base+time.Second {
			t.Fatalf("Delay(%d) = %v >= base+1s %v", attempt, d, base+time.Second)
		}
	})
}
