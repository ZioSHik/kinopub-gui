// Package scheduler implements the download job scheduler with bounded
// concurrency, rate limiting, retry/backoff, and graceful shutdown (Req 4, 5).
package scheduler

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/backoff"
	"github.com/niazlv/kinopub-downloader/internal/lib/ratelimit"
)

// Config holds the scheduler's tunable parameters.
type Config struct {
	MaxConcurrency int           // [1,16], default 2
	MaxRetries     int           // default 5
	MinIntervalMS  int           // [0,60000] ms for rate limiting
	GracePeriod    time.Duration // default 30s
}

// Scheduler implements domain.Scheduler.
type Scheduler struct {
	cfg     Config
	clock   domain.Clock
	logger  domain.Logger
	rng     *rand.Rand
	limiter *ratelimit.Interval
}

// New creates a Scheduler with the given configuration and injectable
// dependencies. It clamps configuration values to their valid ranges.
func New(cfg Config, clock domain.Clock, logger domain.Logger, rng *rand.Rand) *Scheduler {
	// Clamp maxConcurrency to [1,16].
	if cfg.MaxConcurrency < 1 {
		cfg.MaxConcurrency = 1
	}
	if cfg.MaxConcurrency > 16 {
		cfg.MaxConcurrency = 16
	}

	// Default maxRetries to 5 if not set.
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}

	// Default grace period to 30s.
	if cfg.GracePeriod <= 0 {
		cfg.GracePeriod = 30 * time.Second
	}

	limiter := ratelimit.NewInterval(clock, cfg.MinIntervalMS)

	return &Scheduler{
		cfg:     cfg,
		clock:   clock,
		logger:  logger.Component("scheduler"),
		rng:     rng,
		limiter: limiter,
	}
}

// Run executes all jobs with bounded concurrency, rate limiting, retry/backoff,
// and graceful shutdown on ctx cancellation (Req 4, 5).
func (s *Scheduler) Run(ctx context.Context, jobs []domain.Job, exec domain.JobExecutor) domain.RunSummary {
	if len(jobs) == 0 {
		return domain.RunSummary{}
	}

	// Worker pool size = min(maxConcurrency, len(jobs)).
	poolSize := s.cfg.MaxConcurrency
	if len(jobs) < poolSize {
		poolSize = len(jobs)
	}

	// Channel for dispatching jobs to workers. Unbuffered so that dispatch
	// blocks until a worker is ready, enabling cancellation to stop dispatch.
	jobCh := make(chan domain.Job)

	// Collect outcomes.
	var mu sync.Mutex
	outcomes := make([]domain.JobOutcome, 0, len(jobs))

	// Create a cancellable context for graceful shutdown.
	// When the parent ctx is cancelled, we stop dispatching and start the
	// grace period for running workers.
	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()

	// Monitor parent context cancellation for graceful shutdown.
	graceDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Parent cancelled (e.g., SIGINT). Start grace period.
			s.logger.Info("interrupt received, stopping dispatch and starting grace period",
				domain.F("grace_period", s.cfg.GracePeriod.String()))
			// After grace period, cancel the run context to force-stop workers.
			select {
			case <-s.clock.After(s.cfg.GracePeriod):
				s.logger.Warn("grace period elapsed, cancelling remaining jobs")
				runCancel()
			case <-graceDone:
				// All workers finished within grace period.
			}
		case <-graceDone:
			// Normal completion, no shutdown needed.
		}
	}()

	// Start workers.
	var wg sync.WaitGroup
	for i := 0; i < poolSize; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				outcome := s.executeWithRetry(runCtx, job, exec)
				mu.Lock()
				outcomes = append(outcomes, outcome)
				mu.Unlock()
			}
		}()
	}

	// Dispatch jobs. Stop dispatching if parent ctx is cancelled.
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			// Stop dispatching new jobs on interrupt (Req 4.6).
			s.logger.Info("stopping dispatch due to context cancellation")
			goto doneDispatching
		default:
		}

		select {
		case jobCh <- job:
		case <-ctx.Done():
			s.logger.Info("stopping dispatch due to context cancellation")
			goto doneDispatching
		}
	}

doneDispatching:
	close(jobCh)

	// Wait for all workers to finish.
	wg.Wait()
	close(graceDone)

	// Build summary.
	mu.Lock()
	defer mu.Unlock()

	summary := domain.RunSummary{
		Total:    len(jobs),
		Outcomes: outcomes,
	}
	for _, o := range outcomes {
		if o.Succeeded {
			summary.Succeeded++
		} else {
			summary.Failed++
		}
	}

	return summary
}

// executeWithRetry runs a single job with rate limiting and retry/backoff.
func (s *Scheduler) executeWithRetry(ctx context.Context, job domain.Job, exec domain.JobExecutor) domain.JobOutcome {
	outcome := domain.JobOutcome{
		Key: job.Episode.Key,
	}

	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		// Check context before each attempt.
		if ctx.Err() != nil {
			outcome.Err = ctx.Err()
			outcome.Attempts = attempt
			return outcome
		}

		// Rate limit before each attempt (Req 4.5).
		s.limiter.Wait()

		// Execute the job.
		outcome.Attempts = attempt + 1
		err := exec.Execute(ctx, job)
		if err == nil {
			outcome.Succeeded = true
			return outcome
		}

		// Classify the error.
		class := ClassifyError(err)

		switch class {
		case domain.ClassNonRetryable:
			// Non-retryable: fail immediately (Req 5.3).
			s.logger.Warn("job failed with non-retryable error",
				domain.F("episode", job.Episode.Key),
				domain.F("error", err.Error()),
				domain.F("attempts", attempt+1))
			outcome.Err = err
			return outcome

		case domain.ClassRetryable:
			if attempt < s.cfg.MaxRetries {
				// Retryable: backoff and retry (Req 5.1, 5.2, 5.4).
				delay := backoff.Delay(attempt, s.rng)
				s.logger.Info("retrying job after backoff",
					domain.F("episode", job.Episode.Key),
					domain.F("attempt", attempt+1),
					domain.F("delay", delay.String()),
					domain.F("error", err.Error()))
				select {
				case <-s.clock.After(delay):
				case <-ctx.Done():
					outcome.Err = ctx.Err()
					return outcome
				}
			} else {
				// Max retries exhausted (Req 5.5).
				s.logger.Error("job failed after max retries",
					domain.F("episode", job.Episode.Key),
					domain.F("error", err.Error()),
					domain.F("attempts", attempt+1))
				outcome.Err = err
				return outcome
			}

		default:
			// Unknown classification — treat as non-retryable.
			outcome.Err = err
			return outcome
		}
	}

	return outcome
}

// ClassifyError classifies an error for retry decisions (Req 5.1-5.3).
// It is a pure function that inspects the error string and typed errors.
//
// Retryable: HTTP 403, 429, 5xx; connection timeout, reset, DNS failure.
// Non-retryable: HTTP 401, 404.
func ClassifyError(err error) domain.ErrorClass {
	if err == nil {
		return domain.ClassNonRetryable
	}

	// Check for typed network errors first.
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return domain.ClassRetryable
		}
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return domain.ClassRetryable
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return domain.ClassRetryable
	}

	// Check error string for HTTP status codes and connection errors.
	errStr := err.Error()

	// Check for HTTP status codes.
	if containsHTTPStatus(errStr, 401) || containsHTTPStatus(errStr, 404) {
		return domain.ClassNonRetryable
	}

	if containsHTTPStatus(errStr, 403) ||
		containsHTTPStatus(errStr, 429) ||
		containsHTTP5xx(errStr) {
		return domain.ClassRetryable
	}

	// Check for connection-level errors by string matching.
	lower := strings.ToLower(errStr)
	if strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "dns") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "i/o timeout") {
		return domain.ClassRetryable
	}

	// Default: non-retryable.
	return domain.ClassNonRetryable
}

// containsHTTPStatus checks if the error string contains a reference to the
// given HTTP status code.
func containsHTTPStatus(errStr string, code int) bool {
	codeStr := strconv.Itoa(code)
	return strings.Contains(errStr, codeStr)
}

// containsHTTP5xx checks if the error string contains any 5xx status code.
func containsHTTP5xx(errStr string) bool {
	// Check for common 5xx codes.
	for code := 500; code <= 599; code++ {
		if strings.Contains(errStr, strconv.Itoa(code)) {
			return true
		}
	}
	return false
}

// Verify that *Scheduler satisfies domain.Scheduler at compile time.
var _ domain.Scheduler = (*Scheduler)(nil)
