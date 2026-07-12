package kinopub

import (
	"context"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// Canceling a queued episode drops it from the run: it never starts, the run
// finishes WITHOUT it (unlike a pause, which keeps the run alive), and it is
// tallied as failed ("canceled") while its siblings succeed.
func TestRunHLS_CancelQueuedEpisode(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	k2 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 2}
	g := newGatedHLS(k1, k2)

	e, _, ss := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(2)})
	cancelCh := make(chan domain.EpisodeKey, 4)
	e.deps.CancelRequests = cancelCh

	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1 // E02 stays queued behind E01

	done := make(chan domain.RunResult, 1)
	go func() {
		res, _ := e.runHLS(context.Background(), cfg)
		done <- res
	}()

	// E01 is downloading; cancel the still-queued E02.
	select {
	case <-g.started[k1]:
	case <-time.After(2 * time.Second):
		t.Fatal("episode 1 never started")
	}
	cancelCh <- k2
	time.Sleep(200 * time.Millisecond) // let the control goroutine apply it
	close(g.release[k1])

	select {
	case res := <-done:
		if res.Succeeded != 1 {
			t.Errorf("Succeeded = %d, want 1", res.Succeeded)
		}
		if res.Failed != 1 {
			t.Errorf("Failed = %d, want 1 (the canceled episode)", res.Failed)
		}
		g.mu.Lock()
		c2 := g.calls[k2]
		g.mu.Unlock()
		if c2 != 0 {
			t.Errorf("canceled episode downloaded %d times, want 0", c2)
		}
		if ss.completed[k2] {
			t.Error("canceled episode must not be marked completed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish — a canceled episode must not keep the run alive")
	}
}

// Canceling an ACTIVELY downloading episode stops just that download and the
// run finishes without re-attempting it; siblings are unaffected.
func TestRunHLS_CancelActiveEpisode(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	k2 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 2}
	g := newGatedHLS(k1, k2)
	close(g.release[k2]) // E02 completes as soon as it runs

	e, _, ss := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(2)})
	cancelCh := make(chan domain.EpisodeKey, 4)
	e.deps.CancelRequests = cancelCh

	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1

	done := make(chan domain.RunResult, 1)
	go func() {
		res, _ := e.runHLS(context.Background(), cfg)
		done <- res
	}()

	// E01 is mid-download (blocked on its gate) — cancel it.
	select {
	case <-g.started[k1]:
	case <-time.After(2 * time.Second):
		t.Fatal("episode 1 never started")
	}
	cancelCh <- k1

	select {
	case res := <-done:
		if res.Succeeded != 1 {
			t.Errorf("Succeeded = %d, want 1 (E02)", res.Succeeded)
		}
		if res.Failed != 1 {
			t.Errorf("Failed = %d, want 1 (the canceled E01)", res.Failed)
		}
		g.mu.Lock()
		c1 := g.calls[k1]
		g.mu.Unlock()
		if c1 != 1 {
			t.Errorf("canceled episode attempted %d times, want exactly 1 (no re-attempt)", c1)
		}
		if ss.completed[k1] {
			t.Error("canceled episode must not be marked completed")
		}
		if !ss.completed[k2] {
			t.Error("sibling episode must complete normally")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish after canceling the active episode")
	}
}
