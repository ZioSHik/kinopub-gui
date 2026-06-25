package gui

import (
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestEpKey(t *testing.T) {
	got := epKey(domain.EpisodeKey{Season: 2, Episode: 13})
	if got != "S2E13" {
		t.Errorf("epKey = %q, want S2E13", got)
	}
}

func TestContainsKey(t *testing.T) {
	keys := []domain.EpisodeKey{{Season: 1, Episode: 1}, {Season: 2, Episode: 3}}
	if !containsKey(keys, domain.EpisodeKey{Season: 2, Episode: 3}) {
		t.Error("should find existing key")
	}
	// Series field is ignored — only season/episode matter.
	if !containsKey(keys, domain.EpisodeKey{Series: "x", Season: 1, Episode: 1}) {
		t.Error("should match ignoring Series")
	}
	if containsKey(keys, domain.EpisodeKey{Season: 9, Episode: 9}) {
		t.Error("should not find absent key")
	}
	if containsKey(nil, domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Error("nil slice contains nothing")
	}
}

func TestJobFinished(t *testing.T) {
	for _, st := range []string{statusCompleted, statusFailed, statusCanceled} {
		j := &Job{status: st}
		if !j.finished() {
			t.Errorf("status %q should be finished", st)
		}
	}
	for _, st := range []string{statusQueued, statusRunning, statusResolving, statusPaused} {
		j := &Job{status: st}
		if j.finished() {
			t.Errorf("status %q should NOT be finished", st)
		}
	}
}

func TestJobSnapshotSortsEpisodes(t *testing.T) {
	j := newJob("j1", "url", domain.RunConfig{})
	j.episodes["S2E1"] = &EpisodeView{Key: "S2E1", Season: 2, Episode: 1}
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2}
	j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1}
	view := j.snapshot()
	got := []string{view.Episodes[0].Key, view.Episodes[1].Key, view.Episodes[2].Key}
	want := []string{"S1E1", "S1E2", "S2E1"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("episode order = %v, want %v", got, want)
		}
	}
}

func TestJobAddLogTrims(t *testing.T) {
	j := newJob("j1", "url", domain.RunConfig{})
	for i := 0; i < maxJobLogs+50; i++ {
		j.addLog(LogEntry{Message: "x"})
	}
	j.mu.Lock()
	n := len(j.logs)
	j.mu.Unlock()
	if n != maxJobLogs {
		t.Errorf("log buffer = %d, want capped at %d", n, maxJobLogs)
	}
}

func TestManagerListSortsByCreatedDescending(t *testing.T) {
	m := newJobManager(newHub())
	old := newJob("old", "u", domain.RunConfig{})
	old.createdAt = time.Unix(100, 0)
	recent := newJob("new", "u", domain.RunConfig{})
	recent.createdAt = time.Unix(200, 0)
	m.add(old)
	m.add(recent)
	views := m.list()
	if len(views) != 2 || views[0].ID != "new" {
		t.Errorf("list should be newest-first, got %v", []string{views[0].ID, views[1].ID})
	}
}

func TestManagerRemoveAndClearFinished(t *testing.T) {
	m := newJobManager(newHub())

	running := newJob("run", "u", domain.RunConfig{})
	running.status = statusRunning
	m.add(running)
	done := newJob("done", "u", domain.RunConfig{})
	done.status = statusCompleted
	m.add(done)

	// Removing a running job is refused.
	removed, exists := m.remove("run")
	if removed || !exists {
		t.Errorf("running job remove = (%v,%v), want (false,true)", removed, exists)
	}
	// Removing an unknown job.
	removed, exists = m.remove("nope")
	if removed || exists {
		t.Errorf("unknown remove = (%v,%v), want (false,false)", removed, exists)
	}
	// Removing a finished job succeeds.
	removed, exists = m.remove("done")
	if !removed || !exists {
		t.Errorf("finished remove = (%v,%v), want (true,true)", removed, exists)
	}

	// clearFinished removes only finished jobs.
	m.add(newJobWith("c1", statusCompleted))
	m.add(newJobWith("c2", statusFailed))
	m.add(newJobWith("c3", statusRunning))
	n := m.clearFinished()
	if n != 2 {
		t.Errorf("clearFinished = %d, want 2", n)
	}
	if _, ok := m.get("c3"); !ok {
		t.Error("the running job must survive clearFinished")
	}
}

func newJobWith(id, status string) *Job {
	j := newJob(id, "u", domain.RunConfig{})
	j.status = status
	return j
}

func TestManagerOutputPathsDedup(t *testing.T) {
	m := newJobManager(newHub())
	a := newJob("a", "u", domain.RunConfig{OutputPath: "/out/x"})
	a.outputPath = "/out/x"
	b := newJob("b", "u", domain.RunConfig{OutputPath: "/out/x"})
	b.outputPath = "/out/x" // duplicate
	c := newJob("c", "u", domain.RunConfig{})
	c.outputPath = "" // empty skipped
	d := newJob("d", "u", domain.RunConfig{OutputPath: "/out/y"})
	d.outputPath = "/out/y"
	for _, j := range []*Job{a, b, c, d} {
		m.add(j)
	}
	paths := m.outputPaths()
	seen := map[string]int{}
	for _, p := range paths {
		seen[p]++
	}
	if seen["/out/x"] != 1 || seen["/out/y"] != 1 || seen[""] != 0 {
		t.Errorf("outputPaths dedup wrong: %v", paths)
	}
}

func TestPrioritizeEpisode_NotRunning(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusCompleted)
	m.add(j)
	if m.prioritizeEpisode("j", domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Error("prioritizeEpisode on a finished job should fail")
	}
	if m.prioritizeEpisode("missing", domain.EpisodeKey{}) {
		t.Error("prioritizeEpisode on unknown job should fail")
	}
}

func TestPrioritizeEpisode_RunningSends(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	m.add(j)
	key := domain.EpisodeKey{Season: 1, Episode: 2}
	if !m.prioritizeEpisode("j", key) {
		t.Fatal("prioritizeEpisode on a running job should succeed")
	}
	select {
	case got := <-j.prioritize:
		if got != key {
			t.Errorf("delivered key = %+v, want %+v", got, key)
		}
	default:
		t.Error("expected the key to be delivered to the prioritize channel")
	}
}

func TestPauseEpisode(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epPending, SpeedBps: 10, ETASeconds: 5}
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epCompleted}
	m.add(j)

	// Pausable: pending episode flips to paused and the request is queued.
	if !m.pauseEpisode("j", domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Fatal("pausing a pending episode should succeed")
	}
	if j.episodes["S1E1"].State != epPaused || j.episodes["S1E1"].SpeedBps != 0 {
		t.Errorf("S1E1 should be paused with cleared speed: %+v", j.episodes["S1E1"])
	}
	select {
	case <-j.pauseEp:
	default:
		t.Error("pause request not delivered")
	}

	// Not pausable: a completed episode.
	if m.pauseEpisode("j", domain.EpisodeKey{Season: 1, Episode: 2}) {
		t.Error("pausing a completed episode should fail")
	}
	// Unknown episode.
	if m.pauseEpisode("j", domain.EpisodeKey{Season: 9, Episode: 9}) {
		t.Error("pausing an unknown episode should fail")
	}
}

func TestResumeEpisode(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epPaused}
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epRunning}
	m.add(j)

	if !m.resumeEpisode("j", domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Fatal("resuming a paused episode should succeed")
	}
	if j.episodes["S1E1"].State != epPending {
		t.Errorf("resumed episode should go pending, got %q", j.episodes["S1E1"].State)
	}
	select {
	case <-j.resumeEp:
	default:
		t.Error("resume request not delivered")
	}
	// A running episode is not resumable.
	if m.resumeEpisode("j", domain.EpisodeKey{Season: 1, Episode: 2}) {
		t.Error("resuming a running episode should fail")
	}
}

func TestRetryEpisodeLive(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epFailed, Error: "boom", Percent: 50, SpeedBps: 9}
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epCompleted}
	j.episodes["S1E3"] = &EpisodeView{Key: "S1E3", Season: 1, Episode: 3, State: epRunning}
	m.add(j)

	// Failed → retriable; the row is optimistically reset.
	if !m.retryEpisodeLive("j", domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Fatal("retrying a failed episode should succeed")
	}
	ev := j.episodes["S1E1"]
	if ev.State != epPending || ev.Error != "" || ev.Percent != 0 || ev.SpeedBps != 0 {
		t.Errorf("retried row not reset: %+v", ev)
	}
	select {
	case <-j.retryEp:
	default:
		t.Error("retry request not delivered")
	}
	// Completed episode must never be live-retried.
	if m.retryEpisodeLive("j", domain.EpisodeKey{Season: 1, Episode: 2}) {
		t.Error("retrying a completed episode should fail")
	}
	// Currently-running episode must not be retried.
	if m.retryEpisodeLive("j", domain.EpisodeKey{Season: 1, Episode: 3}) {
		t.Error("retrying a running episode should fail")
	}
}

func TestAnswerAudio(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	ch := make(chan []int, 1)
	j.audioAnswer = ch
	m.add(j)

	if !m.answerAudio("j", []int{0, 2}) {
		t.Fatal("answerAudio should deliver when a channel is registered")
	}
	select {
	case got := <-ch:
		if len(got) != 2 || got[0] != 0 || got[1] != 2 {
			t.Errorf("delivered indices = %v", got)
		}
	default:
		t.Error("selection not delivered")
	}

	// No pending request (channel cleared) → false.
	j.audioAnswer = nil
	if m.answerAudio("j", []int{1}) {
		t.Error("answerAudio with no pending channel should fail")
	}
	// Unknown job → false.
	if m.answerAudio("missing", nil) {
		t.Error("answerAudio for unknown job should fail")
	}
}

func TestChooserCanceledByDone(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	done := make(chan struct{})
	j.done = done
	m.add(j)
	c := newGUIChooser(m, j)

	close(done) // simulate the job being canceled while ChooseAudio waits
	sel, err := c.ChooseAudio([]domain.AudioTrackInfo{{Index: 0}}, 5*time.Second)
	if err == nil {
		t.Error("ChooseAudio should return context.Canceled when done is closed")
	}
	if sel != nil {
		t.Errorf("canceled choose should return nil selection, got %v", sel)
	}
	// pendingAudio is cleared on return.
	j.mu.Lock()
	pending := j.pendingAudio
	j.mu.Unlock()
	if pending != nil {
		t.Error("pendingAudio should be cleared after ChooseAudio returns")
	}
}

func TestChooserDeliversSelection(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	j.done = make(chan struct{})
	m.add(j)
	c := newGUIChooser(m, j)

	resultCh := make(chan []int, 1)
	go func() {
		sel, _ := c.ChooseAudio([]domain.AudioTrackInfo{{Index: 0}, {Index: 1}}, 5*time.Second)
		resultCh <- sel
	}()

	// Wait until the chooser has published its pending request, then answer.
	deadline := time.After(2 * time.Second)
	for {
		j.mu.Lock()
		ready := j.audioAnswer != nil
		j.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-deadline:
			t.Fatal("chooser never registered its answer channel")
		case <-time.After(time.Millisecond):
		}
	}
	if !m.answerAudio("j", []int{1}) {
		t.Fatal("answerAudio should succeed")
	}
	select {
	case sel := <-resultCh:
		if len(sel) != 1 || sel[0] != 1 {
			t.Errorf("ChooseAudio returned %v, want [1]", sel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ChooseAudio did not return after the answer")
	}
}

func TestRerunJobEpisode_WidensQueuedScope(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusQueued)
	j.retryOnly = []domain.EpisodeKey{{Season: 1, Episode: 1}}
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epFailed, Error: "x"}
	m.add(j)

	// Already queued → widen the existing scope; do not re-queue.
	if !m.rerunJobEpisode("j", domain.EpisodeKey{Season: 1, Episode: 2}) {
		t.Fatal("rerunJobEpisode on a queued job should succeed")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if !containsKey(j.retryOnly, domain.EpisodeKey{Season: 1, Episode: 2}) {
		t.Errorf("scope not widened: %+v", j.retryOnly)
	}
	if len(j.retryOnly) != 2 {
		t.Errorf("retryOnly = %+v, want 2 entries", j.retryOnly)
	}
	if j.episodes["S1E2"].State != epPending || j.episodes["S1E2"].Error != "" {
		t.Errorf("row not reset: %+v", j.episodes["S1E2"])
	}
}

func TestRerunJobEpisode_FinishedScopesToOne(t *testing.T) {
	m := newJobManager(newHub())
	m.startFn = func(*Job) {} // dispatch is a no-op
	m.setMaxActive(0)
	j := newJobWith("j", statusFailed)
	fin := time.Now()
	j.finishedAt = &fin
	m.add(j)

	if !m.rerunJobEpisode("j", domain.EpisodeKey{Season: 3, Episode: 4}) {
		t.Fatal("rerunJobEpisode on a finished job should succeed")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status != statusQueued || j.finishedAt != nil || j.errMsg != "" {
		t.Errorf("finished job not re-queued cleanly: status=%q finishedAt=%v err=%q", j.status, j.finishedAt, j.errMsg)
	}
	if len(j.retryOnly) != 1 || j.retryOnly[0].Episode != 4 {
		t.Errorf("retryOnly should scope to just the one episode, got %+v", j.retryOnly)
	}
}

func TestRerunJobEpisode_RunningRejected(t *testing.T) {
	m := newJobManager(newHub())
	j := newJobWith("j", statusRunning)
	m.add(j)
	if m.rerunJobEpisode("j", domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Error("rerunJobEpisode on a running job should be rejected (use live retry)")
	}
}
