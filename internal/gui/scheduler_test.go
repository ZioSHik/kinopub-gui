package gui

import (
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// newTestManager builds a JobManager whose startFn records dispatch order and
// mimics a running engine (sets status + an installed cancel) without actually
// downloading. dispatch() invokes startFn synchronously, so assertions can run
// right after submit()/jobFinished() with no sleeps.
func newTestManager() (*JobManager, *[]string, *sync.Mutex) {
	m := newJobManager(newHub())
	var mu sync.Mutex
	started := []string{}
	m.startFn = func(j *Job) {
		mu.Lock()
		started = append(started, j.id)
		mu.Unlock()
		j.mu.Lock()
		j.status = statusRunning
		j.cancel = func() {} // mimic run() installing its context cancel
		j.mu.Unlock()
	}
	return m, &started, &mu
}

func newTestJob(m *JobManager) *Job {
	return newJob(m.nextID(), "https://kino.watch/item/view/1", domain.RunConfig{InputURL: "x"})
}

func snapshotStarted(mu *sync.Mutex, started *[]string) []string {
	mu.Lock()
	defer mu.Unlock()
	out := make([]string, len(*started))
	copy(out, *started)
	return out
}

func TestSchedulerLimitQueuesExtras(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(1)

	a, b, c := newTestJob(m), newTestJob(m), newTestJob(m)
	m.submit(a, false)
	m.submit(b, false)
	m.submit(c, false)

	if got := snapshotStarted(mu, started); len(got) != 1 || got[0] != a.id {
		t.Fatalf("with limit 1, only %s should start; got %v", a.id, got)
	}
	m.mu.RLock()
	pending := len(m.pending)
	m.mu.RUnlock()
	if pending != 2 {
		t.Fatalf("want 2 jobs queued, got %d", pending)
	}

	// Finishing the running job dispatches the next in FIFO order.
	m.jobFinished(a)
	if got := snapshotStarted(mu, started); len(got) != 2 || got[1] != b.id {
		t.Fatalf("after A finished, B should start next; got %v", got)
	}
	m.jobFinished(b)
	if got := snapshotStarted(mu, started); len(got) != 3 || got[2] != c.id {
		t.Fatalf("after B finished, C should start; got %v", got)
	}
}

func TestSchedulerPrioritizeReordersQueue(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(1)

	a, b, c := newTestJob(m), newTestJob(m), newTestJob(m)
	m.submit(a, false) // runs
	m.submit(b, false) // queued
	m.submit(c, false) // queued

	if !m.prioritizeJob(c.id) {
		t.Fatal("prioritizeJob(c) should succeed for a queued job")
	}
	// A running job cannot be prioritized (not waiting).
	if m.prioritizeJob(a.id) {
		t.Fatal("prioritizeJob(a) should fail — A is running, not queued")
	}

	m.jobFinished(a) // A done → C should jump ahead of B
	if got := snapshotStarted(mu, started); got[len(got)-1] != c.id {
		t.Fatalf("prioritized C should start before B; order = %v", got)
	}
}

func TestSchedulerCancelQueuedJob(t *testing.T) {
	m, _, _ := newTestManager()
	m.setMaxActive(1)

	a, b := newTestJob(m), newTestJob(m)
	m.submit(a, false) // runs
	m.submit(b, false) // queued

	if !m.cancelJob(b.id) {
		t.Fatal("cancelJob(b) should report handled")
	}
	b.mu.Lock()
	st := b.status
	b.mu.Unlock()
	if st != statusCanceled {
		t.Fatalf("queued job B should be canceled, got status %q", st)
	}
	m.mu.RLock()
	pending := len(m.pending)
	m.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("canceled job should be removed from the queue; pending=%d", pending)
	}

	// Finishing A must NOT resurrect the canceled B (running stays balanced).
	m.jobFinished(a)
	m.mu.RLock()
	running := m.running
	m.mu.RUnlock()
	if running != 0 {
		t.Fatalf("running counter drifted: %d", running)
	}
}

func TestSchedulerFrontJumpsQueue(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(1)

	a, b, retry := newTestJob(m), newTestJob(m), newTestJob(m)
	m.submit(a, false)    // runs
	m.submit(b, false)    // queued
	m.submit(retry, true) // queued at the FRONT (mimics a per-episode retry)

	m.jobFinished(a) // A done → the front-inserted retry runs before B
	if got := snapshotStarted(mu, started); got[len(got)-1] != retry.id {
		t.Fatalf("front-submitted job should run before B; order = %v", got)
	}
}

func TestSchedulerUnlimitedRunsAll(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(0) // unlimited (default)

	for i := 0; i < 4; i++ {
		m.submit(newTestJob(m), false)
	}
	if got := snapshotStarted(mu, started); len(got) != 4 {
		t.Fatalf("unlimited limit should start all 4 immediately; got %v", got)
	}
}

func TestSettleUnfinishedEpisodes(t *testing.T) {
	mk := func(state string, errMsg string) *EpisodeView {
		return &EpisodeView{State: state, Error: errMsg, SpeedBps: 5, ETASeconds: 9}
	}
	j := &Job{episodes: map[string]*EpisodeView{
		"done":     mk(epCompleted, ""),
		"failed":   mk(epFailed, "boom"),
		"deferred": mk(epDeferred, "audio track 0: segment 3 failed: context canceled"),
		"running":  mk(epRunning, ""),
		"pending":  mk(epPending, ""),
	}}

	settleUnfinishedEpisodesLocked(j, true) // canceled run

	// Completed stays completed; everything transient becomes failed.
	if j.episodes["done"].State != epCompleted {
		t.Errorf("completed episode must stay completed, got %q", j.episodes["done"].State)
	}
	for _, k := range []string{"failed", "deferred", "running", "pending"} {
		if j.episodes[k].State != epFailed {
			t.Errorf("%s episode should settle to failed, got %q", k, j.episodes[k].State)
		}
	}
	// Only the genuinely transient episodes are touched (speed/eta cleared); an
	// already-failed row is left exactly as-is.
	for _, k := range []string{"deferred", "running", "pending"} {
		if j.episodes[k].SpeedBps != 0 || j.episodes[k].ETASeconds != 0 {
			t.Errorf("%s episode should have speed/eta cleared", k)
		}
	}
	// Existing error text is preserved; only error-less episodes get "canceled".
	if j.episodes["deferred"].Error == "canceled" {
		t.Error("deferred episode's real error must not be overwritten")
	}
	if j.episodes["failed"].Error != "boom" {
		t.Errorf("failed episode error changed: %q", j.episodes["failed"].Error)
	}
	if j.episodes["running"].Error != "canceled" || j.episodes["pending"].Error != "canceled" {
		t.Error("error-less transient episodes should be stamped 'canceled' on a canceled run")
	}

	// A non-canceled finish must not stamp the "canceled" reason.
	j2 := &Job{episodes: map[string]*EpisodeView{"pending": mk(epPending, "")}}
	settleUnfinishedEpisodesLocked(j2, false)
	if j2.episodes["pending"].State != epFailed || j2.episodes["pending"].Error != "" {
		t.Errorf("non-canceled finish: got state=%q err=%q", j2.episodes["pending"].State, j2.episodes["pending"].Error)
	}
}

func TestSchedulerPauseResumeQueuedJob(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(1)

	a, b := newTestJob(m), newTestJob(m)
	m.submit(a, false) // runs
	m.submit(b, false) // queued

	// Pause the queued job: it leaves the dispatch queue and becomes "paused".
	if !m.pauseJob(b.id) {
		t.Fatal("pauseJob(b) should succeed for a queued job")
	}
	b.mu.Lock()
	st := b.status
	b.mu.Unlock()
	if st != statusPaused {
		t.Fatalf("queued job should be paused, got %q", st)
	}
	m.mu.RLock()
	pending := len(m.pending)
	m.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("paused job must leave the queue; pending=%d", pending)
	}

	// Finishing A must NOT start the paused B.
	m.jobFinished(a)
	if got := snapshotStarted(mu, started); len(got) != 1 {
		t.Fatalf("paused job must not be dispatched; started=%v", got)
	}

	// Resume re-queues B; with a now-free slot it dispatches.
	if !m.resumeJob(b.id) {
		t.Fatal("resumeJob(b) should succeed for a paused job")
	}
	if got := snapshotStarted(mu, started); len(got) != 2 || got[1] != b.id {
		t.Fatalf("resumed job should be dispatched; started=%v", got)
	}

	// Resume/pause guards.
	if m.resumeJob(b.id) {
		t.Error("resumeJob on a non-paused job should fail")
	}
}

func TestSchedulerUrgentBypassesLimit(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(1)

	a := newTestJob(m)
	m.submit(a, false) // fills the only slot
	if got := snapshotStarted(mu, started); len(got) != 1 {
		t.Fatalf("want A running, got %v", got)
	}

	// A normal job must wait (no slot).
	b := newTestJob(m)
	m.submit(b, false)
	if got := snapshotStarted(mu, started); len(got) != 1 {
		t.Fatalf("normal job must queue behind the limit, got %v", got)
	}

	// An urgent (front) job — a per-episode retry — starts immediately despite
	// the slot being full.
	urgent := newTestJob(m)
	m.submit(urgent, true)
	got := snapshotStarted(mu, started)
	if len(got) != 2 || got[1] != urgent.id {
		t.Fatalf("urgent job should bypass the limit and start now, got %v", got)
	}
	// The non-urgent B is still waiting.
	m.mu.RLock()
	pending := len(m.pending)
	m.mu.RUnlock()
	if pending != 1 {
		t.Fatalf("normal job B should still be queued, pending=%d", pending)
	}
}

func TestSchedulerPauseRejectsFinished(t *testing.T) {
	m, _, _ := newTestManager()
	m.setMaxActive(0)
	j := newTestJob(m)
	j.mu.Lock()
	j.status = statusCompleted
	j.mu.Unlock()
	m.add(j)
	if m.pauseJob(j.id) {
		t.Error("pauseJob on a completed job should fail")
	}
	if m.resumeJob(j.id) {
		t.Error("resumeJob on a completed job should fail")
	}
}

func TestSettlePausedEpisodes(t *testing.T) {
	mk := func(state, errMsg string) *EpisodeView {
		return &EpisodeView{State: state, Error: errMsg, SpeedBps: 7, ETASeconds: 3}
	}
	j := &Job{episodes: map[string]*EpisodeView{
		"done":     mk(epCompleted, ""),
		"failed":   mk(epFailed, "boom"),
		"running":  mk(epRunning, ""),
		"deferred": mk(epDeferred, "net glitch"),
		"pending":  mk(epPending, ""),
	}}
	settlePausedEpisodesLocked(j)

	if j.episodes["done"].State != epCompleted {
		t.Errorf("completed must stay completed, got %q", j.episodes["done"].State)
	}
	if j.episodes["failed"].State != epFailed {
		t.Errorf("already-failed must stay failed, got %q", j.episodes["failed"].State)
	}
	for _, k := range []string{"running", "deferred", "pending"} {
		if j.episodes[k].State != epPaused {
			t.Errorf("%s should settle to paused, got %q", k, j.episodes[k].State)
		}
		if j.episodes[k].SpeedBps != 0 || j.episodes[k].ETASeconds != 0 {
			t.Errorf("%s should have speed/eta cleared", k)
		}
	}
	// Pause must never stamp an error (it isn't a failure).
	if j.episodes["deferred"].Error != "net glitch" {
		t.Errorf("deferred error should be preserved, got %q", j.episodes["deferred"].Error)
	}
	if j.episodes["pending"].Error != "" {
		t.Errorf("paused pending must not gain an error, got %q", j.episodes["pending"].Error)
	}
}

func TestSchedulerRaisingLimitDispatchesQueued(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(1)

	m.submit(newTestJob(m), false) // runs
	m.submit(newTestJob(m), false) // queued
	if got := snapshotStarted(mu, started); len(got) != 1 {
		t.Fatalf("want 1 running under limit 1, got %v", got)
	}
	m.setMaxActive(2) // headroom → the queued job dispatches
	if got := snapshotStarted(mu, started); len(got) != 2 {
		t.Fatalf("raising the limit should dispatch the queued job; got %v", got)
	}
}

// --- adaptive admission ----------------------------------------------------

// idleFor folds n idle samples into the gauge.
func idleFor(g *admissionGauge, n int) {
	for i := 0; i < n; i++ {
		g.observe(true)
	}
}

// busyFor folds n samples where the pipe was fully claimed.
func busyFor(g *admissionGauge, n int) {
	for i := 0; i < n; i++ {
		g.observe(false)
	}
}

func TestAdmissionHoldsOneSlotUntilTheWindowIsFull(t *testing.T) {
	var g admissionGauge
	idleFor(&g, admissionWindowSamples-1)
	if got := g.slots(); got != maxActiveDownloads {
		t.Errorf("slots = %d on a partial window, want %d — the first seconds of a job are just its resolve", got, maxActiveDownloads)
	}
	// The sample that fills the window is the one that may grant the slot.
	idleFor(&g, 1)
	if got := g.slots(); got != maxAdaptiveDownloads {
		t.Errorf("slots = %d, want %d once a full window of idle is in", got, maxAdaptiveDownloads)
	}
}

func TestAdmissionKeepsOneSlotWhileThePipeIsClaimed(t *testing.T) {
	var g admissionGauge
	busyFor(&g, admissionWindowSamples*2)
	if got := g.slots(); got != maxActiveDownloads {
		t.Errorf("slots = %d with the pipe saturated, want %d — a second title would only divide it", got, maxActiveDownloads)
	}
}

func TestAdmissionThresholdIsTheIdleShare(t *testing.T) {
	// 9 of 15 samples idle is 60%, exactly the share that buys the slot.
	var at admissionGauge
	idleFor(&at, 9)
	busyFor(&at, 6)
	if got := at.slots(); got != maxAdaptiveDownloads {
		t.Errorf("slots = %d at the threshold, want %d", got, maxAdaptiveDownloads)
	}

	var under admissionGauge
	idleFor(&under, 8)
	busyFor(&under, 7)
	if got := under.slots(); got != maxActiveDownloads {
		t.Errorf("slots = %d just under the threshold, want %d", got, maxActiveDownloads)
	}
}

func TestAdmissionWindowSlidesSoTheSlotIsGivenBack(t *testing.T) {
	var g admissionGauge
	idleFor(&g, admissionWindowSamples)
	if g.slots() != maxAdaptiveDownloads {
		t.Fatalf("precondition: a full idle window should grant the second slot")
	}
	// The title starts downloading in earnest; the idle history ages out.
	busyFor(&g, admissionWindowSamples)
	if got := g.slots(); got != maxActiveDownloads {
		t.Errorf("slots = %d after the window filled with busy samples, want %d", got, maxActiveDownloads)
	}
}

func TestAdmissionResetForgetsTheWindow(t *testing.T) {
	var g admissionGauge
	idleFor(&g, admissionWindowSamples)
	g.reset() // e.g. the CDN started throttling, or nothing is running
	if got := g.slots(); got != maxActiveDownloads {
		t.Errorf("slots = %d after a reset, want %d", got, maxActiveDownloads)
	}
}

func TestSchedulerSerializesRenditionsWithoutBlockingOtherTitles(t *testing.T) {
	m, started, mu := newTestManager()
	m.setMaxActive(0)
	a, b, other := newTestJob(m), newTestJob(m), newTestJob(m)
	a.cfg.Quality = "1080p"
	b.cfg.Quality = "720p"
	other.cfg.InputURL = "other-title"
	m.submit(a, false)
	m.submit(b, false)
	m.submit(other, false)
	got := snapshotStarted(mu, started)
	if len(got) != 2 || got[0] != a.id || got[1] != other.id {
		t.Fatalf("variant must wait while another title starts: %v", got)
	}
	m.jobFinished(a)
	got = snapshotStarted(mu, started)
	if len(got) != 3 || got[2] != b.id {
		t.Fatalf("new rendition did not start after original finished: %v", got)
	}
}
