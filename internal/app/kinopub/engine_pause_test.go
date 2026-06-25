package kinopub

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// gatedHLS blocks each DownloadEpisode until the test releases that episode's
// key, and signals when a download actually starts. This lets a test hold an
// episode "in flight" while it pauses a different, not-yet-started one.
type gatedHLS struct {
	mu      sync.Mutex
	started map[domain.EpisodeKey]chan struct{}
	release map[domain.EpisodeKey]chan struct{}
	calls   map[domain.EpisodeKey]int
}

func newGatedHLS(keys ...domain.EpisodeKey) *gatedHLS {
	g := &gatedHLS{
		started: make(map[domain.EpisodeKey]chan struct{}),
		release: make(map[domain.EpisodeKey]chan struct{}),
		calls:   make(map[domain.EpisodeKey]int),
	}
	for _, k := range keys {
		g.started[k] = make(chan struct{}, 1)
		g.release[k] = make(chan struct{})
	}
	return g
}

func (g *gatedHLS) DownloadEpisode(ctx context.Context, _ string, _ domain.Quality, _ string, key domain.EpisodeKey, _ domain.ProgressSink) (*domain.HLSDownloadResult, error) {
	g.mu.Lock()
	g.calls[key]++
	st, rel := g.started[key], g.release[key]
	g.mu.Unlock()
	if st != nil {
		select {
		case st <- struct{}{}:
		default:
		}
	}
	select {
	case <-rel:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &domain.HLSDownloadResult{Resolution: "1280x720", BitrateKbps: 2000, Codec: "h264", VideoPath: "/tmp/v.ts"}, nil
}

func (g *gatedHLS) ListAudioTracks(context.Context, string, domain.Quality) ([]domain.AudioTrackInfo, error) {
	return nil, nil
}
func (g *gatedHLS) SetAudioPreference(domain.AudioPreference) {}

// A paused episode is held aside (not downloaded) while its siblings proceed,
// keeps the run alive, and downloads after it is resumed.
func TestRunHLS_PerEpisodePauseResume(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	k2 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 2}
	g := newGatedHLS(k1, k2)

	e, _, ss := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(2)})
	pause := make(chan domain.EpisodeKey, 8)
	resume := make(chan domain.EpisodeKey, 8)
	e.deps.PauseRequests = pause
	e.deps.ResumeRequests = resume

	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1 // one episode at a time → deterministic ordering

	done := make(chan domain.RunResult, 1)
	go func() {
		res, _ := e.runHLS(context.Background(), cfg)
		done <- res
	}()

	// Hold E02 before it can start (it is queued behind E01).
	pause <- k2

	// E01 runs to completion.
	select {
	case <-g.started[k1]:
	case <-time.After(2 * time.Second):
		t.Fatal("episode 1 never started")
	}
	close(g.release[k1])

	// E02 must NOT start while paused — the run stays alive waiting on it.
	select {
	case <-g.started[k2]:
		t.Fatal("paused episode 2 should not have started")
	case <-time.After(300 * time.Millisecond):
	}

	// Resume E02 → it now downloads.
	resume <- k2
	select {
	case <-g.started[k2]:
	case <-time.After(2 * time.Second):
		t.Fatal("resumed episode 2 did not start")
	}
	close(g.release[k2])

	select {
	case res := <-done:
		if res.Succeeded != 2 {
			t.Errorf("Succeeded = %d, want 2", res.Succeeded)
		}
		if !ss.completed[k1] || !ss.completed[k2] {
			t.Errorf("both episodes should be completed in state: %+v", ss.completed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish after resume")
	}
}

// Pausing an episode that is ACTIVELY downloading stops just that download
// (its own context is canceled), holds it without failing the run, and a resume
// re-attempts it to completion.
func TestRunHLS_PauseActiveEpisode(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	g := newGatedHLS(k1)
	e, _, ss := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(1)})
	pause := make(chan domain.EpisodeKey, 4)
	resume := make(chan domain.EpisodeKey, 4)
	e.deps.PauseRequests = pause
	e.deps.ResumeRequests = resume

	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1
	done := make(chan domain.RunResult, 1)
	go func() {
		res, _ := e.runHLS(context.Background(), cfg)
		done <- res
	}()

	// E01 is downloading (blocked on its gate). Pause it mid-flight.
	select {
	case <-g.started[k1]:
	case <-time.After(2 * time.Second):
		t.Fatal("episode 1 never started")
	}
	pause <- k1

	// The run must stay alive (episode held, not failed, not completed).
	select {
	case <-done:
		t.Fatal("run ended while the active episode was paused")
	case <-time.After(400 * time.Millisecond):
	}

	// Resume → it is re-attempted; let the re-attempt finish.
	resume <- k1
	select {
	case <-g.started[k1]:
	case <-time.After(2 * time.Second):
		t.Fatal("resumed episode did not re-start")
	}
	close(g.release[k1])

	select {
	case res := <-done:
		if res.Succeeded != 1 {
			t.Errorf("Succeeded = %d, want 1", res.Succeeded)
		}
		if res.Failed != 0 {
			t.Errorf("Failed = %d, want 0 (pause is not a failure)", res.Failed)
		}
		if !ss.completed[k1] {
			t.Error("episode should be completed after resume")
		}
		g.mu.Lock()
		calls := g.calls[k1]
		g.mu.Unlock()
		if calls < 2 {
			t.Errorf("expected a re-attempt after resume, got %d download calls", calls)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish after resume")
	}
}

// A live retry of an episode that already SUCCEEDED must be a no-op: it must not
// re-download the file or double-count the success.
func TestRunHLS_LiveRetryOfSucceededIsNoop(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	k2 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 2}
	g := newGatedHLS(k1, k2)
	close(g.release[k1]) // E01 completes immediately; E02 stays gated to keep the run alive

	e, _, _ := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(2)})
	retry := make(chan domain.EpisodeKey, 4)
	e.deps.RetryRequests = retry

	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1 // sequential: E01 fully completes before E02 starts
	done := make(chan domain.RunResult, 1)
	go func() {
		res, _ := e.runHLS(context.Background(), cfg)
		done <- res
	}()

	<-g.started[k1] // E01 ran (and, release being closed, completed)
	<-g.started[k2] // E02 started → E01's success is already recorded

	// Duplicate retry for the already-succeeded E01 — must be ignored.
	retry <- k1
	time.Sleep(200 * time.Millisecond)
	close(g.release[k2])

	select {
	case res := <-done:
		if res.Succeeded != 2 {
			t.Errorf("Succeeded = %d, want 2 (no double count)", res.Succeeded)
		}
		g.mu.Lock()
		c1 := g.calls[k1]
		g.mu.Unlock()
		if c1 != 1 {
			t.Errorf("E01 downloaded %d times, want 1 (retry of a succeeded episode must be a no-op)", c1)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish")
	}
}

// A whole-job pause (context cancel while deps.Paused() is true) must NOT count
// the preserved-for-resume episodes as failures — otherwise the paused job's
// summary would read "N failed" for episodes that resume and succeed.
func TestRunHLS_WholeJobPauseNoFailures(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	g := newGatedHLS(k1) // only E01 is gated; E02/E03 never start before the pause
	e, _, _ := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(3)})

	var paused atomic.Bool
	e.deps.Paused = paused.Load

	ctx, cancel := context.WithCancel(context.Background())
	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1

	done := make(chan domain.RunResult, 1)
	go func() {
		res, _ := e.runHLS(ctx, cfg)
		done <- res
	}()

	// Wait until E01 is downloading, then pause the whole job.
	select {
	case <-g.started[k1]:
	case <-time.After(2 * time.Second):
		t.Fatal("episode 1 never started")
	}
	paused.Store(true) // mark pause BEFORE cancel so the sweep preserves
	cancel()

	select {
	case res := <-done:
		if res.Failed != 0 {
			t.Errorf("paused run must report 0 failures, got Failed=%d (preserved episodes wrongly counted)", res.Failed)
		}
		if res.Succeeded != 0 {
			t.Errorf("no episode completed before pause, got Succeeded=%d", res.Succeeded)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish after pause")
	}
}
