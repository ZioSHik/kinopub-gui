package kinopub

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestIsTransientDownloadError(t *testing.T) {
	transient := []string{
		"video track: segment 44 failed: after 5 attempts: context deadline exceeded",
		"video track: segment 44 failed: after 5 attempts: unexpected EOF",
		"read tcp: connection reset by peer",
		"dial tcp: connection refused",
		"master playlist returned HTTP 502",
		"media playlist returned HTTP 503",
		"HTTP 504",
		"HTTP 429",
		"net/http: request canceled (Client.Timeout exceeded)",
		"write: broken pipe",
		"lookup cdn.example.com: no such host",
	}
	for _, msg := range transient {
		if !isTransientDownloadError(errors.New(msg)) {
			t.Errorf("expected transient: %q", msg)
		}
	}

	permanent := []string{
		"no variants found in master playlist",
		"quality selection: no variants available",
		"downloader does not support HLS muxing",
		"HTTP 404",
		"HTTP 403",
		"invalid manifest",
	}
	for _, msg := range permanent {
		if isTransientDownloadError(errors.New(msg)) {
			t.Errorf("expected permanent: %q", msg)
		}
	}

	if isTransientDownloadError(nil) {
		t.Error("nil error must not be transient")
	}
}

func TestIsTransientDownloadError_Wrapped(t *testing.T) {
	base := errors.New("context deadline exceeded")
	wrapped := fmt.Errorf("video track: %w", fmt.Errorf("segment 44 failed: %w", base))
	if !isTransientDownloadError(wrapped) {
		t.Fatal("wrapped transient error should be detected via its message")
	}
}

func TestEpisodeRetryBackoff(t *testing.T) {
	// Grows with attempts.
	if a, b := episodeRetryBackoff(1), episodeRetryBackoff(2); a >= b {
		t.Errorf("backoff should grow: attempt1=%s attempt2=%s", a, b)
	}
	// Capped at 3 minutes.
	if got := episodeRetryBackoff(100); got > 3*time.Minute {
		t.Errorf("backoff should cap at 3m, got %s", got)
	}
	if got := episodeRetryBackoff(1); got <= 0 {
		t.Errorf("backoff for attempt 1 should be positive, got %s", got)
	}
}

func TestReadyDeferredIndex(t *testing.T) {
	now := time.Now()
	q := []*pendingEpisode{
		{nextAt: now.Add(time.Minute)},  // not ready
		{nextAt: now.Add(-time.Second)}, // ready (due)
		{nextAt: now.Add(-time.Minute)}, // ready (earlier)
	}
	idx := readyDeferredIndex(q, now)
	if idx != 2 {
		t.Fatalf("readyDeferredIndex = %d, want 2 (earliest due)", idx)
	}

	// None ready.
	q2 := []*pendingEpisode{{nextAt: now.Add(time.Hour)}}
	if idx := readyDeferredIndex(q2, now); idx != -1 {
		t.Fatalf("readyDeferredIndex (none ready) = %d, want -1", idx)
	}

	// Empty.
	if idx := readyDeferredIndex(nil, now); idx != -1 {
		t.Fatalf("readyDeferredIndex (empty) = %d, want -1", idx)
	}
}

func TestEarliestDeferredIndex(t *testing.T) {
	now := time.Now()
	q := []*pendingEpisode{
		{nextAt: now.Add(2 * time.Minute)},
		{nextAt: now.Add(30 * time.Second)},
		{nextAt: now.Add(5 * time.Minute)},
	}
	if idx := earliestDeferredIndex(q); idx != 1 {
		t.Fatalf("earliestDeferredIndex = %d, want 1", idx)
	}
}

// recordingReporter captures the deferred-retry lifecycle for assertions.
type recordingReporter struct {
	started   []domain.EpisodeKey
	completed []domain.EpisodeKey
	failed    []domain.EpisodeKey
	deferred  []domain.EpisodeKey
}

func (r *recordingReporter) Start(domain.SeriesPlan)                 {}
func (r *recordingReporter) EpisodeStarted(k domain.EpisodeKey)      { r.started = append(r.started, k) }
func (r *recordingReporter) TrackProgress(domain.EpisodeKey, domain.TrackRef, int) {}
func (r *recordingReporter) EpisodeCompleted(k domain.EpisodeKey)    { r.completed = append(r.completed, k) }
func (r *recordingReporter) EpisodeFailed(k domain.EpisodeKey, _ error) {
	r.failed = append(r.failed, k)
}
func (r *recordingReporter) EpisodeDeferred(k domain.EpisodeKey, _ error, _ int) {
	r.deferred = append(r.deferred, k)
}
func (r *recordingReporter) Stop() {}

func TestReportEpisodeDeferred_OptionalHook(t *testing.T) {
	rec := &recordingReporter{}
	key := domain.EpisodeKey{Season: 1, Episode: 5}
	reportEpisodeDeferred(rec, key, context.DeadlineExceeded, 2)
	if len(rec.deferred) != 1 || rec.deferred[0] != key {
		t.Fatalf("expected deferred hook to fire once for %v, got %v", key, rec.deferred)
	}

	// A reporter without the hook must not panic.
	reportEpisodeDeferred(&mockProgressReporter{}, key, context.DeadlineExceeded, 2)
}
