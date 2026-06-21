package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// **Validates: Requirements 4.1, 4.3, 4.4, 5.1, 5.2, 5.3, 5.5, 5.7**

// TestProperty11_ConcurrencyNeverExceedsMax verifies that for any
// maxConcurrency ∈ [1,16] and any number of jobs [1,50], the observed peak
// concurrency during Run never exceeds maxConcurrency.
//
// **Validates: Requirements 4.1**
func TestProperty11_ConcurrencyNeverExceedsMax(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxConc := rapid.IntRange(1, 16).Draw(t, "maxConcurrency")
		numJobs := rapid.IntRange(1, 50).Draw(t, "numJobs")

		clock := newFakeClock()
		rng := rand.New(rand.NewSource(42))
		sched := New(Config{
			MaxConcurrency: maxConc,
			MaxRetries:     0,
			MinIntervalMS:  0,
			GracePeriod:    30 * time.Second,
		}, clock, nopLogger{}, rng)

		jobs := makeJobs(numJobs)

		var currentConc int64
		var peakConc int64

		exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
			cur := atomic.AddInt64(&currentConc, 1)
			// Update peak concurrency atomically.
			for {
				old := atomic.LoadInt64(&peakConc)
				if cur <= old || atomic.CompareAndSwapInt64(&peakConc, old, cur) {
					break
				}
			}
			// Simulate brief work to allow concurrency to build up.
			time.Sleep(100 * time.Microsecond)
			atomic.AddInt64(&currentConc, -1)
			return nil
		}}

		sched.Run(context.Background(), jobs, exec)

		observed := atomic.LoadInt64(&peakConc)
		if observed > int64(maxConc) {
			t.Fatalf("peak concurrency %d exceeds configured max %d (numJobs=%d)",
				observed, maxConc, numJobs)
		}
	})
}

// TestProperty12_EveryJobRunsExactlyOnceWithOutcome verifies that for any set
// of jobs where all succeed, Run returns a RunSummary with
// len(Outcomes) == len(jobs), all Succeeded==true, and Total==len(jobs).
//
// **Validates: Requirements 4.3, 4.4, 5.7**
func TestProperty12_EveryJobRunsExactlyOnceWithOutcome(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxConc := rapid.IntRange(1, 16).Draw(t, "maxConcurrency")
		numJobs := rapid.IntRange(1, 50).Draw(t, "numJobs")

		clock := newFakeClock()
		rng := rand.New(rand.NewSource(42))
		sched := New(Config{
			MaxConcurrency: maxConc,
			MaxRetries:     0,
			MinIntervalMS:  0,
			GracePeriod:    30 * time.Second,
		}, clock, nopLogger{}, rng)

		jobs := makeJobs(numJobs)

		// Count how many times each job is executed.
		execCounts := make([]int64, numJobs)

		exec := &fakeExecutor{fn: func(_ context.Context, job domain.Job) error {
			idx := job.Episode.Key.Episode - 1 // Episodes are 1-indexed in makeJobs.
			atomic.AddInt64(&execCounts[idx], 1)
			return nil
		}}

		summary := sched.Run(context.Background(), jobs, exec)

		// Verify Total == numJobs.
		if summary.Total != numJobs {
			t.Fatalf("Total = %d, want %d", summary.Total, numJobs)
		}

		// Verify len(Outcomes) == numJobs.
		if len(summary.Outcomes) != numJobs {
			t.Fatalf("len(Outcomes) = %d, want %d", len(summary.Outcomes), numJobs)
		}

		// Verify all outcomes succeeded.
		for i, o := range summary.Outcomes {
			if !o.Succeeded {
				t.Fatalf("Outcome[%d] Succeeded = false, want true", i)
			}
		}

		// Verify each job was executed exactly once.
		for i, count := range execCounts {
			c := atomic.LoadInt64(&count)
			if c != 1 {
				t.Fatalf("job %d executed %d times, want exactly 1", i+1, c)
			}
		}
	})
}

// TestProperty14_RetryClassificationCorrectness verifies that for any error
// string containing "403", "429", or 5xx patterns, ClassifyError returns
// ClassRetryable. For "401" or "404", returns ClassNonRetryable.
//
// **Validates: Requirements 5.1, 5.2, 5.3**
func TestProperty14_RetryClassificationCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random prefix/suffix to wrap around the status code.
		prefix := rapid.StringMatching(`[a-zA-Z ]{0,20}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-zA-Z ]{0,20}`).Draw(t, "suffix")

		// Test retryable codes: 403, 429, and 5xx range.
		retryableCode := rapid.SampledFrom([]int{403, 429, 500, 501, 502, 503, 504, 520, 599}).Draw(t, "retryableCode")
		retryableErr := fmt.Errorf("%s%d%s", prefix, retryableCode, suffix)
		got := ClassifyError(retryableErr)
		if got != domain.ClassRetryable {
			t.Fatalf("ClassifyError(%q) = %d, want ClassRetryable for code %d",
				retryableErr.Error(), got, retryableCode)
		}

		// Test non-retryable codes: 401, 404.
		nonRetryableCode := rapid.SampledFrom([]int{401, 404}).Draw(t, "nonRetryableCode")
		nonRetryableErr := fmt.Errorf("%s%d%s", prefix, nonRetryableCode, suffix)
		got = ClassifyError(nonRetryableErr)
		if got != domain.ClassNonRetryable {
			t.Fatalf("ClassifyError(%q) = %d, want ClassNonRetryable for code %d",
				nonRetryableErr.Error(), got, nonRetryableCode)
		}
	})
}

// TestProperty15_RetryCountBoundedByMax verifies that for any maxRetries ∈ [1,10]
// and a job that always fails with a retryable error, the total attempts ==
// maxRetries + 1 (initial + retries).
//
// **Validates: Requirements 5.5**
func TestProperty15_RetryCountBoundedByMax(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxRetries := rapid.IntRange(1, 10).Draw(t, "maxRetries")

		clock := newFakeClock()
		rng := rand.New(rand.NewSource(42))
		sched := New(Config{
			MaxConcurrency: 1,
			MaxRetries:     maxRetries,
			MinIntervalMS:  0,
			GracePeriod:    30 * time.Second,
		}, clock, nopLogger{}, rng)

		jobs := makeJobs(1)

		var totalAttempts int64
		// Always fail with a retryable error (HTTP 503).
		exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
			atomic.AddInt64(&totalAttempts, 1)
			return errors.New("HTTP 503 service unavailable")
		}}

		summary := sched.Run(context.Background(), jobs, exec)

		expectedAttempts := int64(maxRetries + 1) // initial + retries
		observed := atomic.LoadInt64(&totalAttempts)
		if observed != expectedAttempts {
			t.Fatalf("total attempts = %d, want %d (maxRetries=%d)",
				observed, expectedAttempts, maxRetries)
		}

		// Verify the outcome records the correct attempt count.
		if len(summary.Outcomes) != 1 {
			t.Fatalf("len(Outcomes) = %d, want 1", len(summary.Outcomes))
		}
		if summary.Outcomes[0].Attempts != int(expectedAttempts) {
			t.Fatalf("Outcome.Attempts = %d, want %d",
				summary.Outcomes[0].Attempts, expectedAttempts)
		}

		// Verify the job is marked as failed.
		if summary.Outcomes[0].Succeeded {
			t.Fatalf("Outcome.Succeeded = true, want false (job always fails)")
		}
		if summary.Failed != 1 {
			t.Fatalf("Failed = %d, want 1", summary.Failed)
		}
	})
}
