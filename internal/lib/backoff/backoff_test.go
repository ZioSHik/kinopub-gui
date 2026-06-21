package backoff

import (
	"math/rand"
	"testing"
	"time"
)

func TestDelay_ExponentialGrowth(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	tests := []struct {
		attempt     int
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{0, 1 * time.Second, 2 * time.Second},   // base=1s, +jitter<1s
		{1, 2 * time.Second, 3 * time.Second},   // base=2s
		{2, 4 * time.Second, 5 * time.Second},   // base=4s
		{3, 8 * time.Second, 9 * time.Second},   // base=8s
		{4, 16 * time.Second, 17 * time.Second}, // base=16s
		{5, 32 * time.Second, 33 * time.Second}, // base=32s
	}

	for _, tc := range tests {
		d := Delay(tc.attempt, rng)
		if d < tc.expectedMin || d >= tc.expectedMax {
			t.Errorf("Delay(%d) = %v, want [%v, %v)", tc.attempt, d, tc.expectedMin, tc.expectedMax)
		}
	}
}

func TestDelay_CappedAt60s(t *testing.T) {
	rng := rand.New(rand.NewSource(0))

	// attempt=6 → base would be 64s, capped to 60s
	for _, attempt := range []int{6, 7, 10, 20, 100} {
		d := Delay(attempt, rng)
		if d < 60*time.Second || d >= 61*time.Second {
			t.Errorf("Delay(%d) = %v, want [60s, 61s)", attempt, d)
		}
	}
}

func TestDelay_Deterministic(t *testing.T) {
	// Same seed should produce same results
	rng1 := rand.New(rand.NewSource(123))
	rng2 := rand.New(rand.NewSource(123))

	for attempt := 0; attempt < 10; attempt++ {
		d1 := Delay(attempt, rng1)
		d2 := Delay(attempt, rng2)
		if d1 != d2 {
			t.Errorf("Delay(%d) not deterministic: %v != %v", attempt, d1, d2)
		}
	}
}

func TestDelay_JitterRange(t *testing.T) {
	// Run many iterations to verify jitter stays in [0, 1s)
	rng := rand.New(rand.NewSource(99))

	for i := 0; i < 1000; i++ {
		d := Delay(0, rng) // base=1s
		jitter := d - 1*time.Second
		if jitter < 0 || jitter >= 1*time.Second {
			t.Errorf("iteration %d: jitter = %v, want [0, 1s)", i, jitter)
		}
	}
}
