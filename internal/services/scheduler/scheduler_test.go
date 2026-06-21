package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// --- Test helpers ---

// fakeClock implements domain.Clock for deterministic testing.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
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
	// For tests, fire immediately to avoid blocking.
	go func() {
		c.Sleep(d)
		ch <- c.Now()
	}()
	return ch
}

// nopLogger implements domain.Logger as a no-op.
type nopLogger struct{}

func (nopLogger) Debug(_ string, _ ...domain.Field) {}
func (nopLogger) Info(_ string, _ ...domain.Field)  {}
func (nopLogger) Warn(_ string, _ ...domain.Field)  {}
func (nopLogger) Error(_ string, _ ...domain.Field) {}
func (l nopLogger) With(_ ...domain.Field) domain.Logger {
	return l
}
func (l nopLogger) Component(_ string) domain.Logger {
	return l
}

// fakeExecutor implements domain.JobExecutor for testing.
type fakeExecutor struct {
	fn func(ctx context.Context, job domain.Job) error
}

func (e *fakeExecutor) Execute(ctx context.Context, job domain.Job) error {
	return e.fn(ctx, job)
}

func makeJobs(n int) []domain.Job {
	jobs := make([]domain.Job, n)
	for i := range jobs {
		jobs[i] = domain.Job{
			Episode: domain.Episode{
				Key: domain.EpisodeKey{
					Series:  "test-series",
					Season:  1,
					Episode: i + 1,
				},
			},
			OutPath: fmt.Sprintf("/tmp/ep%d.mkv", i+1),
		}
	}
	return jobs
}

func newTestScheduler(cfg Config) *Scheduler {
	clock := newFakeClock()
	rng := rand.New(rand.NewSource(42))
	return New(cfg, clock, nopLogger{}, rng)
}

// --- Tests ---

func TestRun_AllJobsSucceed(t *testing.T) {
	sched := newTestScheduler(Config{MaxConcurrency: 2, MaxRetries: 3})
	jobs := makeJobs(5)

	exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
		return nil
	}}

	summary := sched.Run(context.Background(), jobs, exec)

	if summary.Total != 5 {
		t.Errorf("Total = %d, want 5", summary.Total)
	}
	if summary.Succeeded != 5 {
		t.Errorf("Succeeded = %d, want 5", summary.Succeeded)
	}
	if summary.Failed != 0 {
		t.Errorf("Failed = %d, want 0", summary.Failed)
	}
	if len(summary.Outcomes) != 5 {
		t.Errorf("len(Outcomes) = %d, want 5", len(summary.Outcomes))
	}
}

func TestRun_EmptyJobs(t *testing.T) {
	sched := newTestScheduler(Config{MaxConcurrency: 2, MaxRetries: 3})

	exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
		t.Fatal("should not be called")
		return nil
	}}

	summary := sched.Run(context.Background(), nil, exec)

	if summary.Total != 0 {
		t.Errorf("Total = %d, want 0", summary.Total)
	}
}

func TestRun_NonRetryableError_FailsImmediately(t *testing.T) {
	sched := newTestScheduler(Config{MaxConcurrency: 1, MaxRetries: 5})
	jobs := makeJobs(1)

	callCount := 0
	exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
		callCount++
		return errors.New("HTTP 404 not found")
	}}

	summary := sched.Run(context.Background(), jobs, exec)

	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retries for 404)", callCount)
	}
	if summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", summary.Failed)
	}
	if summary.Outcomes[0].Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", summary.Outcomes[0].Attempts)
	}
}

func TestRun_RetryableError_RetriesUpToMax(t *testing.T) {
	sched := newTestScheduler(Config{MaxConcurrency: 1, MaxRetries: 3})
	jobs := makeJobs(1)

	callCount := 0
	exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
		callCount++
		return errors.New("HTTP 503 service unavailable")
	}}

	summary := sched.Run(context.Background(), jobs, exec)

	// Should be called 1 initial + 3 retries = 4 times.
	if callCount != 4 {
		t.Errorf("callCount = %d, want 4", callCount)
	}
	if summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", summary.Failed)
	}
	if summary.Outcomes[0].Attempts != 4 {
		t.Errorf("Attempts = %d, want 4", summary.Outcomes[0].Attempts)
	}
}

func TestRun_RetryableError_SucceedsOnRetry(t *testing.T) {
	sched := newTestScheduler(Config{MaxConcurrency: 1, MaxRetries: 5})
	jobs := makeJobs(1)

	callCount := 0
	exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
		callCount++
		if callCount < 3 {
			return errors.New("HTTP 429 too many requests")
		}
		return nil
	}}

	summary := sched.Run(context.Background(), jobs, exec)

	if callCount != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}
	if summary.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", summary.Succeeded)
	}
}

func TestRun_FailedJobDoesNotAbortOthers(t *testing.T) {
	sched := newTestScheduler(Config{MaxConcurrency: 1, MaxRetries: 0})
	jobs := makeJobs(3)

	callCount := 0
	exec := &fakeExecutor{fn: func(_ context.Context, job domain.Job) error {
		callCount++
		if job.Episode.Key.Episode == 2 {
			return errors.New("HTTP 404 not found")
		}
		return nil
	}}

	summary := sched.Run(context.Background(), jobs, exec)

	if callCount != 3 {
		t.Errorf("callCount = %d, want 3 (all jobs should run)", callCount)
	}
	if summary.Succeeded != 2 {
		t.Errorf("Succeeded = %d, want 2", summary.Succeeded)
	}
	if summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", summary.Failed)
	}
}

func TestRun_ConcurrencyBounded(t *testing.T) {
	maxConc := 3
	sched := newTestScheduler(Config{MaxConcurrency: maxConc, MaxRetries: 0})
	jobs := makeJobs(10)

	var currentConc int64
	var maxObserved int64

	exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
		cur := atomic.AddInt64(&currentConc, 1)
		// Track max observed concurrency.
		for {
			old := atomic.LoadInt64(&maxObserved)
			if cur <= old || atomic.CompareAndSwapInt64(&maxObserved, old, cur) {
				break
			}
		}
		// Simulate some work.
		time.Sleep(1 * time.Millisecond)
		atomic.AddInt64(&currentConc, -1)
		return nil
	}}

	summary := sched.Run(context.Background(), jobs, exec)

	observed := atomic.LoadInt64(&maxObserved)
	if observed > int64(maxConc) {
		t.Errorf("max observed concurrency = %d, want <= %d", observed, maxConc)
	}
	if summary.Total != 10 {
		t.Errorf("Total = %d, want 10", summary.Total)
	}
	if summary.Succeeded != 10 {
		t.Errorf("Succeeded = %d, want 10", summary.Succeeded)
	}
}

func TestRun_ContextCancellation_StopsDispatch(t *testing.T) {
	clock := newFakeClock()
	rng := rand.New(rand.NewSource(42))
	// Use concurrency 1 and a slow executor so that cancellation can
	// interrupt the dispatch loop before all jobs are sent to the channel.
	sched := New(Config{MaxConcurrency: 1, MaxRetries: 0, GracePeriod: 50 * time.Millisecond}, clock, nopLogger{}, rng)
	jobs := makeJobs(100) // Many jobs to ensure dispatch loop is still running.

	ctx, cancel := context.WithCancel(context.Background())
	var callCount int64

	exec := &fakeExecutor{fn: func(_ context.Context, _ domain.Job) error {
		count := atomic.AddInt64(&callCount, 1)
		if count >= 3 {
			cancel() // Cancel after 3 jobs.
		}
		// Simulate work so the worker is busy and the channel fills up.
		time.Sleep(5 * time.Millisecond)
		return nil
	}}

	summary := sched.Run(ctx, jobs, exec)

	// Not all 100 jobs should have run because dispatch was stopped.
	count := atomic.LoadInt64(&callCount)
	if count >= 100 {
		t.Errorf("callCount = %d, expected fewer than 100 due to cancellation", count)
	}
	// Total should still be 100 (the number of submitted jobs).
	if summary.Total != 100 {
		t.Errorf("Total = %d, want 100", summary.Total)
	}
}

// --- ClassifyError tests ---

func TestClassifyError_Nil(t *testing.T) {
	if got := ClassifyError(nil); got != domain.ClassNonRetryable {
		t.Errorf("ClassifyError(nil) = %d, want ClassNonRetryable", got)
	}
}

func TestClassifyError_HTTP401(t *testing.T) {
	err := errors.New("server returned HTTP 401 unauthorized")
	if got := ClassifyError(err); got != domain.ClassNonRetryable {
		t.Errorf("ClassifyError(401) = %d, want ClassNonRetryable", got)
	}
}

func TestClassifyError_HTTP404(t *testing.T) {
	err := errors.New("HTTP 404 not found")
	if got := ClassifyError(err); got != domain.ClassNonRetryable {
		t.Errorf("ClassifyError(404) = %d, want ClassNonRetryable", got)
	}
}

func TestClassifyError_HTTP403(t *testing.T) {
	err := errors.New("HTTP 403 forbidden")
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(403) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_HTTP429(t *testing.T) {
	err := errors.New("HTTP 429 too many requests")
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(429) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_HTTP500(t *testing.T) {
	err := errors.New("HTTP 500 internal server error")
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(500) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_HTTP502(t *testing.T) {
	err := errors.New("HTTP 502 bad gateway")
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(502) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_ConnectionTimeout(t *testing.T) {
	err := errors.New("connection timeout while connecting to server")
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(timeout) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_ConnectionReset(t *testing.T) {
	err := errors.New("connection reset by peer")
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(reset) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_DNSFailure(t *testing.T) {
	err := &net.DNSError{Err: "no such host", Name: "example.com"}
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(DNS) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_NetTimeout(t *testing.T) {
	err := &timeoutError{}
	if got := ClassifyError(err); got != domain.ClassRetryable {
		t.Errorf("ClassifyError(net.Error timeout) = %d, want ClassRetryable", got)
	}
}

func TestClassifyError_UnknownError(t *testing.T) {
	err := errors.New("something completely unexpected happened")
	if got := ClassifyError(err); got != domain.ClassNonRetryable {
		t.Errorf("ClassifyError(unknown) = %d, want ClassNonRetryable", got)
	}
}

// timeoutError implements net.Error with Timeout() = true.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

// --- Config clamping tests ---

func TestNew_ClampsMaxConcurrency(t *testing.T) {
	clock := newFakeClock()
	rng := rand.New(rand.NewSource(42))

	s := New(Config{MaxConcurrency: 0}, clock, nopLogger{}, rng)
	if s.cfg.MaxConcurrency != 1 {
		t.Errorf("MaxConcurrency = %d, want 1 (clamped from 0)", s.cfg.MaxConcurrency)
	}

	s = New(Config{MaxConcurrency: 100}, clock, nopLogger{}, rng)
	if s.cfg.MaxConcurrency != 16 {
		t.Errorf("MaxConcurrency = %d, want 16 (clamped from 100)", s.cfg.MaxConcurrency)
	}
}

func TestNew_DefaultsMaxRetries(t *testing.T) {
	clock := newFakeClock()
	rng := rand.New(rand.NewSource(42))

	s := New(Config{MaxRetries: 0}, clock, nopLogger{}, rng)
	if s.cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5 (default)", s.cfg.MaxRetries)
	}
}

func TestNew_DefaultsGracePeriod(t *testing.T) {
	clock := newFakeClock()
	rng := rand.New(rand.NewSource(42))

	s := New(Config{GracePeriod: 0}, clock, nopLogger{}, rng)
	if s.cfg.GracePeriod != 30*time.Second {
		t.Errorf("GracePeriod = %v, want 30s (default)", s.cfg.GracePeriod)
	}
}
