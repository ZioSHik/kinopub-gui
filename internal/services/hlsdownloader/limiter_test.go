package hlsdownloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// newTestLimiter builds a limiter whose clock the test drives by hand, so the
// controller's policy can be exercised without sleeping through real windows.
func newTestLimiter(start, min int) (*adaptiveLimiter, func(time.Duration)) {
	l := newAdaptiveLimiter(start, min, nopLogger{})
	now := time.Unix(1_700_000_000, 0)
	l.now = func() time.Time { return now }
	return l, func(d time.Duration) { now = now.Add(d) }
}

// --- construction bounds ---------------------------------------------------

func TestNewAdaptiveLimiterClampsBounds(t *testing.T) {
	cases := []struct {
		name             string
		start, min       int
		wantLimit, wantM int
	}{
		{"start below floor is raised to it", 2, 6, 6, 6},
		{"start above the ceiling is clamped", 99, 1, maxSegmentConcurrency, 1},
		{"floor above the ceiling is clamped too", 1, 99, maxSegmentConcurrency, maxSegmentConcurrency},
		{"zero floor becomes one", 4, 0, 4, 1},
		{"ordinary values pass through", 4, 2, 4, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := newAdaptiveLimiter(c.start, c.min, nopLogger{})
			if l.limit != c.wantLimit {
				t.Errorf("limit = %d, want %d", l.limit, c.wantLimit)
			}
			if l.min != c.wantM {
				t.Errorf("min = %d, want %d", l.min, c.wantM)
			}
		})
	}
}

// --- shared track floor ----------------------------------------------------

func TestTrackFloorFollowsTheActiveEpisodes(t *testing.T) {
	l, _ := newTestLimiter(4, 1)

	// One episode with a video and two audio tracks joins the shared limiter.
	l.addTracks(3)
	if l.min != 3 {
		t.Errorf("floor = %d, want 3 (one slot per track)", l.min)
	}
	if l.limit != 4 {
		t.Errorf("limit = %d, want the controller's 4 left alone — it clears the floor", l.limit)
	}

	// A second episode starts alongside it: the floor has to cover both, so the
	// limit is pushed up to it rather than starving one episode's audio.
	l.addTracks(3)
	if l.min != 6 {
		t.Errorf("floor = %d, want 6 with two 3-track episodes in flight", l.min)
	}
	if l.limit != 6 {
		t.Errorf("limit = %d, want it raised to the new floor of 6", l.limit)
	}

	// The first episode finishes. The floor drops back, but the limit stays where
	// throughput put it — nothing about the link changed.
	l.removeTracks(3)
	if l.min != 3 {
		t.Errorf("floor = %d, want 3 after one episode finished", l.min)
	}
	if l.limit != 6 {
		t.Errorf("limit = %d, want the measured 6 kept", l.limit)
	}

	l.removeTracks(3)
	if l.min != 1 {
		t.Errorf("floor = %d, want the baseline 1 once nothing is downloading", l.min)
	}
}

func TestTrackFloorNeverFallsBelowTheBaseline(t *testing.T) {
	l, _ := newTestLimiter(4, 2)
	l.addTracks(1)
	if l.min != 2 {
		t.Errorf("floor = %d, want the baseline 2 to win over a single track", l.min)
	}
	// Unbalanced removes (a double release) must not drive the floor negative.
	l.removeTracks(5)
	if l.min != 2 {
		t.Errorf("floor = %d, want the baseline 2 held", l.min)
	}
}

func TestTrackFloorLiftsTheCeilingButNotTheHardLimit(t *testing.T) {
	l, _ := newTestLimiter(4, 1)
	l.addTracks(99)
	// Keeping every track moving is a correctness guarantee, so the floor lifts
	// the effective cap out of its way — and stops at the hard one.
	if l.min != hardSegmentConcurrency {
		t.Errorf("floor = %d, want it clamped to %d", l.min, hardSegmentConcurrency)
	}
	if got := l.capLocked(); got != hardSegmentConcurrency {
		t.Errorf("cap = %d, want the floor to have lifted it", got)
	}
	if l.limit != hardSegmentConcurrency {
		t.Errorf("limit = %d, want it raised to the floor", l.limit)
	}
	// The lift was the floor's, not a throughput-proven one: when the episodes
	// leave, the cap comes back down — a later lone download must not inherit
	// headroom it never earned.
	l.removeTracks(99)
	if l.min != 1 {
		t.Errorf("floor = %d, want the baseline 1 restored", l.min)
	}
	if got := l.capLocked(); got != maxSegmentConcurrency {
		t.Errorf("cap = %d, want it back at the default %d", got, maxSegmentConcurrency)
	}
	if l.limit != maxSegmentConcurrency {
		t.Errorf("limit = %d, want the floor-granted headroom given back", l.limit)
	}
}

// One CDN burst fails every in-flight segment at once, and each worker reports
// its own 429; the limit must halve once per burst, not once per report.
func TestThrottleHalvesOncePerBurst(t *testing.T) {
	l, advance := newTestLimiter(16, 1)

	for i := 0; i < 6; i++ {
		l.throttle(time.Second)
	}
	if l.limit != 8 {
		t.Errorf("limit = %d, want a single halving to 8 for one burst", l.limit)
	}

	// A 429 after the cool-down expired is a genuinely new event.
	advance(2 * time.Second)
	l.throttle(time.Second)
	if l.limit != 4 {
		t.Errorf("limit = %d, want 4 after a second, separate event", l.limit)
	}
}

// The measurement window must not span an idle gap: an episode's ffmpeg remux
// (no segments moving) followed by the next episode's first completion used to
// close a window whose elapsed included the whole gap — a phantom throughput
// collapse that shrank the limit at every episode boundary.
func TestIdleGapDoesNotPoisonTheMeasurementWindow(t *testing.T) {
	l, advance := newTestLimiter(6, 1)
	ctx := context.Background()

	// A healthy window closes at a high rate.
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	advance(probeWindow)
	l.done(100 << 20)
	limitBefore := l.current()

	// Everything drains; the pipe sits idle through a long remux.
	advance(30 * time.Second)

	// The next episode's first segment completes at the same healthy rate.
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	advance(probeWindow)
	l.done(100 << 20)

	if got := l.current(); got < limitBefore {
		t.Errorf("limit = %d after an idle gap, want >= %d — idle time is not a throughput drop",
			got, limitBefore)
	}
}

func TestThrottleCannotDropBelowTheActiveTrackFloor(t *testing.T) {
	l, _ := newTestLimiter(8, 1)
	l.addTracks(6) // two episodes' worth of tracks are mid-flight

	l.throttle(2 * time.Second)

	if l.limit != 6 {
		t.Errorf("limit = %d, want the 429 to stop at the floor of 6, not halve past it", l.limit)
	}
}

func TestSharedLimiterIsOnePerDownloader(t *testing.T) {
	d := New(nil, domain.RequestAuth{}, nopLogger{}, WithConcurrency(6))

	first, second := d.sharedLimiter(), d.sharedLimiter()
	if first != second {
		t.Error("every episode must fetch through the same controller, not build its own")
	}
	if got := first.current(); got != 6 {
		t.Errorf("starting limit = %d, want the configured 6", got)
	}
}

// --- hill-climbing policy --------------------------------------------------

func TestControllerClimbsWhileThroughputImproves(t *testing.T) {
	l, advance := newTestLimiter(4, 1)

	// First window has no baseline, so the controller takes a speculative step up.
	l.advanceLocked(1_000_000, l.now())
	if l.limit != 5 {
		t.Fatalf("after first window: limit = %d, want 5", l.limit)
	}

	// Each subsequent window is meaningfully faster → keep climbing.
	rate := 1_000_000.0
	for i := 0; i < 3; i++ {
		advance(probeWindow)
		rate *= 1.5
		l.advanceLocked(rate, l.now())
	}
	if l.limit != 8 {
		t.Errorf("limit = %d, want 8 after three improving windows", l.limit)
	}
}

func TestControllerStepsBackAndSettlesWhenThroughputDrops(t *testing.T) {
	l, advance := newTestLimiter(6, 1)

	advance(probeWindow)
	l.advanceLocked(1_000_000, l.now()) // baseline (speculative step up → 7)
	advance(probeWindow)
	l.advanceLocked(500_000, l.now()) // half the throughput → step back

	if l.limit != 6 {
		t.Fatalf("limit = %d, want 6 after the drop", l.limit)
	}
	if !l.now().Before(l.settledAt) {
		t.Error("a drop should settle the controller, pausing further probing")
	}

	// While settled, even a big improvement must not move the limit.
	advance(probeWindow)
	l.advanceLocked(10_000_000, l.now())
	if l.limit != 6 {
		t.Errorf("limit = %d, want 6 — settled controllers hold still", l.limit)
	}

	// Once the settle window expires, probing resumes.
	advance(settleFor + time.Second)
	l.advanceLocked(20_000_000, l.now())
	if l.limit != 7 {
		t.Errorf("limit = %d, want 7 — probing should resume after settleFor", l.limit)
	}
}

func TestControllerHoldsOnPlateau(t *testing.T) {
	l, advance := newTestLimiter(5, 1)

	advance(probeWindow)
	l.advanceLocked(1_000_000, l.now()) // baseline → 6
	advance(probeWindow)
	l.advanceLocked(1_010_000, l.now()) // within the dead band → this is the knee

	if l.limit != 6 {
		t.Errorf("limit = %d, want 6 held at the plateau", l.limit)
	}
	if !l.now().Before(l.settledAt) {
		t.Error("a plateau should settle the controller")
	}
}

func TestControllerRespectsFloorAndCeiling(t *testing.T) {
	// Ceiling: relentless improvement lifts the controller's own cap — a link
	// that keeps paying for more sockets is exactly the case the default 16 gets
	// wrong — but it must stop dead at the hard limit.
	up, advanceUp := newTestLimiter(maxSegmentConcurrency, 1)
	rate := 1_000_000.0
	for i := 0; i < 80; i++ {
		advanceUp(probeWindow)
		rate *= 1.5
		up.advanceLocked(rate, up.now())
	}
	if up.limit != hardSegmentConcurrency {
		t.Errorf("limit = %d, want the hard ceiling %d", up.limit, hardSegmentConcurrency)
	}
	if up.ceiling != hardSegmentConcurrency {
		t.Errorf("ceiling = %d, want it lifted no further than %d", up.ceiling, hardSegmentConcurrency)
	}

	// Floor: relentless degradation must leave one slot per track.
	down, advanceDown := newTestLimiter(4, 3)
	rate = 10_000_000
	for i := 0; i < 8; i++ {
		advanceDown(settleFor + probeWindow) // outrun the settle timer each round
		rate /= 2
		down.advanceLocked(rate, down.now())
	}
	if down.limit != 3 {
		t.Errorf("limit = %d, want the floor 3 (one slot per track)", down.limit)
	}
}

// --- CDN back-pressure -----------------------------------------------------

func TestThrottleHalvesLimitAndParksWorkers(t *testing.T) {
	l, _ := newTestLimiter(8, 2)

	l.throttle(3 * time.Second)

	if l.limit != 4 {
		t.Errorf("limit = %d, want 8 halved to 4", l.limit)
	}
	if got := l.coolUntil.Sub(l.now()); got != 3*time.Second {
		t.Errorf("cool-down = %v, want the server's 3s Retry-After", got)
	}
	if l.lastRate != 0 {
		t.Error("the window spanning a throttle is meaningless and must be dropped")
	}
	if !l.now().Before(l.settledAt) {
		t.Error("a throttle should stop upward probing for a while")
	}
}

func TestThrottleHonorsLongRetryAfterAndNeverGoesBelowTheFloor(t *testing.T) {
	l, _ := newTestLimiter(3, 3)

	l.throttle(time.Hour)

	if l.limit != 3 {
		t.Errorf("limit = %d, want the floor 3", l.limit)
	}
	if got := l.coolUntil.Sub(l.now()); got != time.Hour {
		t.Errorf("cool-down = %v, want the full Retry-After %v", got, time.Hour)
	}
}

func TestThrottleWithoutHintUsesAMinimumPause(t *testing.T) {
	l, _ := newTestLimiter(8, 1)
	l.throttle(0)
	if got := l.coolUntil.Sub(l.now()); got != time.Second {
		t.Errorf("cool-down = %v, want a 1s default when the server sent no hint", got)
	}
}

// --- gating ----------------------------------------------------------------

func TestAcquireNeverExceedsTheLimit(t *testing.T) {
	l := newAdaptiveLimiter(3, 1, nopLogger{})
	ctx := context.Background()

	var mu sync.Mutex
	live, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.acquire(ctx); err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			mu.Lock()
			live++
			if live > peak {
				peak = live
			}
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			live--
			mu.Unlock()
			l.done(1024)
		}()
	}
	wg.Wait()

	if peak > 3 {
		t.Errorf("peak in-flight = %d, want at most the limit of 3", peak)
	}
	if l.inFlight != 0 {
		t.Errorf("inFlight = %d after everyone finished, want 0", l.inFlight)
	}
}

func TestAcquireReturnsOnContextCancel(t *testing.T) {
	l := newAdaptiveLimiter(1, 1, nopLogger{})
	if err := l.acquire(context.Background()); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// The only slot is taken, so this one has to wait — until the context dies.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.acquire(ctx) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("acquire should fail once its context is cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire did not return after its context was cancelled")
	}
}

func TestAcquireWaitsOutTheCoolDown(t *testing.T) {
	l := newAdaptiveLimiter(4, 1, nopLogger{})
	l.throttle(120 * time.Millisecond)

	start := time.Now()
	if err := l.acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if waited := time.Since(start); waited < 100*time.Millisecond {
		t.Errorf("acquire returned after %v, want it to sit out the ~120ms cool-down", waited)
	}
}

func TestReleaseDoesNotFeedTheController(t *testing.T) {
	l, advance := newTestLimiter(4, 1)

	if err := l.acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	advance(probeWindow * 2)
	l.release()

	if l.windowBytes != 0 || l.windowDone != 0 {
		t.Errorf("a failed attempt must not enter the measurement window: bytes=%d done=%d",
			l.windowBytes, l.windowDone)
	}
	if l.limit != 4 {
		t.Errorf("limit = %d, want 4 — a release alone should not move the controller", l.limit)
	}
}

// --- Retry-After parsing ---------------------------------------------------

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"absent", "", 0},
		{"delta seconds", "7", 7 * time.Second},
		{"zero seconds", "0", 0},
		{"negative seconds", "-5", 0},
		{"http date in the future", now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second},
		{"http date in the past", now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{"garbage", "soon-ish", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseRetryAfter(c.in, now); got != c.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestStatusErrorThrottled(t *testing.T) {
	cases := map[int]bool{
		http.StatusTooManyRequests:     true,
		http.StatusServiceUnavailable:  true,
		http.StatusForbidden:           false,
		http.StatusNotFound:            false,
		http.StatusInternalServerError: false,
	}
	for code, want := range cases {
		if got := (&statusError{Code: code}).throttled(); got != want {
			t.Errorf("HTTP %d throttled = %v, want %v", code, got, want)
		}
	}
}

// --- end to end through the segment fetcher --------------------------------

// A CDN answering 429 should push the limit down and park new work, rather than
// having every worker rediscover the limit on its own.
func TestDownloadSegmentReportsThrottlingToTheLimiter(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("segment-payload"))
	}))
	defer srv.Close()

	d := New(srv.Client(), domain.RequestAuth{}, nopLogger{})
	d.client = srv.Client() // bypass the uTLS browser client for the test server

	lim := newAdaptiveLimiter(8, 1, nopLogger{})
	out := filepath.Join(t.TempDir(), "seg.ts")

	n, err := d.downloadSegment(context.Background(), Segment{URL: srv.URL, Index: 0}, out, lim)
	if err != nil {
		t.Fatalf("downloadSegment: %v", err)
	}
	if n == 0 {
		t.Error("expected the retry to succeed with a non-empty segment")
	}
	if got := lim.current(); got != 4 {
		t.Errorf("limit = %d, want 8 halved to 4 by the 429", got)
	}
	if data, _ := os.ReadFile(out); string(data) != "segment-payload" {
		t.Errorf("segment contents = %q", data)
	}
}

func TestFetchSegmentSurfacesStatusAndRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	d := New(srv.Client(), domain.RequestAuth{}, nopLogger{})
	d.client = srv.Client()

	_, err := d.fetchSegment(context.Background(), srv.URL, filepath.Join(t.TempDir(), "seg.ts"))
	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("error = %T (%v), want *statusError", err, err)
	}
	if se.Code != http.StatusTooManyRequests {
		t.Errorf("Code = %d, want 429", se.Code)
	}
	if se.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want 5s", se.RetryAfter)
	}
}
