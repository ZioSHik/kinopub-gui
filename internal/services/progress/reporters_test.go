package progress

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/logx"
)

// findStrField returns a string field value by key, or nil.
func findStrField(fields []domain.Field, key string) *string {
	for _, f := range fields {
		if f.Key == key {
			if v, ok := f.Value.(string); ok {
				return &v
			}
		}
	}
	return nil
}

func samplePlan() domain.SeriesPlan {
	return domain.SeriesPlan{
		Title:              "Test",
		Total:              10,
		Seasons:            map[int]int{1: 6, 2: 4},
		AlreadyCompleted:   2,
		CompletedPerSeason: map[int]int{1: 2},
	}
}

// ---------------------------------------------------------------------------
// LogReporter state machine
// ---------------------------------------------------------------------------

func TestLogReporter_StartSeedsCompleted(t *testing.T) {
	h := &captureHandler{}
	r := NewLog(logx.New([]logx.Handler{h}))
	r.Start(samplePlan())

	// Start emits one record with totals.
	if len(h.records) != 1 {
		t.Fatalf("want 1 start record, got %d", len(h.records))
	}
	rec := h.records[0]
	if rec.Message != "progress reporting started" {
		t.Fatalf("unexpected start msg: %q", rec.Message)
	}
	if got := findIntField(rec.Fields, "total_episodes"); got == nil || *got != 10 {
		t.Fatalf("total_episodes wrong: %v", got)
	}
	if got := findIntField(rec.Fields, "already_completed"); got == nil || *got != 2 {
		t.Fatalf("already_completed wrong: %v", got)
	}
	if got := findIntField(rec.Fields, "seasons"); got == nil || *got != 2 {
		t.Fatalf("seasons wrong: %v", got)
	}

	// Seeded state: series 2/10 = 20%, season 1 = 2/6 = 33%.
	r.mu.Lock()
	if r.completedTotal != 2 {
		t.Errorf("completedTotal seeded wrong: %d", r.completedTotal)
	}
	if r.seriesPercent() != 20 {
		t.Errorf("series percent seeded wrong: %d", r.seriesPercent())
	}
	if r.seasonPercent(1) != 33 {
		t.Errorf("season1 percent seeded wrong: %d", r.seasonPercent(1))
	}
	r.mu.Unlock()
}

func TestLogReporter_EpisodeLifecycle(t *testing.T) {
	h := &captureHandler{}
	r := NewLog(logx.New([]logx.Handler{h}))
	r.Start(samplePlan())
	h.records = nil

	key := domain.EpisodeKey{Series: "Test", Season: 1, Episode: 3}

	r.EpisodeStarted(key)
	if len(h.records) != 1 || h.records[0].Message != "episode started" {
		t.Fatalf("started record wrong: %+v", h.records)
	}
	// At start, completed unchanged: series 20%, season1 33%.
	if got := findIntField(h.records[0].Fields, "series_percent"); got == nil || *got != 20 {
		t.Fatalf("started series_percent wrong: %v", got)
	}

	r.EpisodeCompleted(key)
	last := h.records[len(h.records)-1]
	if last.Message != "episode completed" {
		t.Fatalf("completed record wrong: %q", last.Message)
	}
	// Now completedTotal=3 -> series 30%, season1 completed=3/6 -> 50%.
	if got := findIntField(last.Fields, "series_percent"); got == nil || *got != 30 {
		t.Fatalf("completed series_percent: %v", got)
	}
	if got := findIntField(last.Fields, "season_percent"); got == nil || *got != 50 {
		t.Fatalf("completed season_percent: %v", got)
	}
}

func TestLogReporter_EpisodeFailed(t *testing.T) {
	h := &captureHandler{}
	r := NewLog(logx.New([]logx.Handler{h}))
	r.Start(samplePlan())
	h.records = nil

	key := domain.EpisodeKey{Series: "Test", Season: 2, Episode: 1}
	r.EpisodeFailed(key, errors.New("boom"))

	if len(h.records) != 1 {
		t.Fatalf("want 1 failed record, got %d", len(h.records))
	}
	rec := h.records[0]
	if rec.Message != "episode failed" {
		t.Fatalf("msg: %q", rec.Message)
	}
	if rec.Level != domain.LevelError {
		t.Fatalf("failed should be error level, got %v", rec.Level)
	}
	if got := findStrField(rec.Fields, "error"); got == nil || *got != "boom" {
		t.Fatalf("error field: %v", got)
	}
	// Failure should NOT advance completed counts.
	r.mu.Lock()
	if r.completedTotal != 2 {
		t.Errorf("failure advanced completedTotal: %d", r.completedTotal)
	}
	r.mu.Unlock()
}

func TestLogReporter_EpisodeDeferred(t *testing.T) {
	h := &captureHandler{}
	r := NewLog(logx.New([]logx.Handler{h}))
	r.Start(samplePlan())
	h.records = nil

	key := domain.EpisodeKey{Series: "Test", Season: 1, Episode: 4}
	r.EpisodeDeferred(key, errors.New("transient"), 2)

	if len(h.records) != 1 {
		t.Fatalf("want 1 record, got %d", len(h.records))
	}
	rec := h.records[0]
	if rec.Level != domain.LevelWarn {
		t.Fatalf("deferred should be warn, got %v", rec.Level)
	}
	if got := findIntField(rec.Fields, "attempts"); got == nil || *got != 2 {
		t.Fatalf("attempts: %v", got)
	}
	if got := findStrField(rec.Fields, "error"); got == nil || *got != "transient" {
		t.Fatalf("error: %v", got)
	}
}

func TestLogReporter_TrackProgressIsNoop(t *testing.T) {
	h := &captureHandler{}
	r := NewLog(logx.New([]logx.Handler{h}))
	r.Start(samplePlan())
	h.records = nil

	r.TrackProgress(domain.EpisodeKey{Season: 1, Episode: 1}, domain.TrackRef{}, 50)
	if len(h.records) != 0 {
		t.Fatalf("TrackProgress should emit nothing, got %d", len(h.records))
	}
}

func TestLogReporter_Stop(t *testing.T) {
	h := &captureHandler{}
	r := NewLog(logx.New([]logx.Handler{h}))
	r.Start(samplePlan())
	h.records = nil
	r.Stop()
	if len(h.records) != 1 || h.records[0].Message != "progress reporting stopped" {
		t.Fatalf("stop record wrong: %+v", h.records)
	}
}

func TestLogReporter_SeasonPercentUnknownSeason(t *testing.T) {
	h := &captureHandler{}
	r := NewLog(logx.New([]logx.Handler{h}))
	r.Start(samplePlan())
	r.mu.Lock()
	defer r.mu.Unlock()
	// Season 99 not in plan -> total 0 -> 0%.
	if got := r.seasonPercent(99); got != 0 {
		t.Fatalf("unknown season should be 0, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// LiveReporter — full render lifecycle through a non-TTY buffer
// ---------------------------------------------------------------------------

// syncBuf is a goroutine-safe buffer (the tick loop writes concurrently).
type syncBuf struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newPlainLive builds a LiveReporter wired to a buffer with isTTY=false so
// rendered output contains no ANSI codes (easy to assert on).
func newPlainLive(w *syncBuf) *LiveReporter {
	r := &LiveReporter{
		w:     w,
		coord: logx.NewCoordinator(w),
		isTTY: false,
	}
	return r
}

func TestLiveReporter_StartSeedsState(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())
	defer r.Stop()

	r.mu.Lock()
	if r.completedTotal != 2 {
		t.Errorf("completedTotal=%d want 2", r.completedTotal)
	}
	if r.completedSeason[1] != 2 {
		t.Errorf("completedSeason[1]=%d want 2", r.completedSeason[1])
	}
	if r.stopped {
		t.Errorf("should not be stopped after start")
	}
	r.mu.Unlock()
}

func TestLiveReporter_EpisodeLifecycleState(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())
	defer r.Stop()

	key := domain.EpisodeKey{Series: "Test", Season: 1, Episode: 3}
	r.EpisodeStarted(key)

	r.mu.Lock()
	if _, ok := r.currentEpisodes[key]; !ok {
		t.Fatalf("episode not registered on start")
	}
	r.mu.Unlock()

	// Feed track progress; episodePercent should reflect it.
	r.TrackProgress(key, domain.TrackRef{Kind: domain.TrackVideo}, 100)
	r.TrackProgress(key, domain.TrackRef{Kind: domain.TrackAudio, Index: 0}, 50)

	r.mu.Lock()
	ep := r.currentEpisodes[key]
	if got := r.episodePercent(ep); got != 75 {
		t.Errorf("episodePercent=%d want 75", got)
	}
	r.mu.Unlock()

	r.EpisodeCompleted(key)
	r.mu.Lock()
	if _, ok := r.currentEpisodes[key]; ok {
		t.Errorf("episode should be removed after completion")
	}
	if r.completedTotal != 3 {
		t.Errorf("completedTotal=%d want 3", r.completedTotal)
	}
	if r.completedSeason[1] != 3 {
		t.Errorf("completedSeason[1]=%d want 3", r.completedSeason[1])
	}
	r.mu.Unlock()
}

func TestLiveReporter_TrackProgressUnknownEpisodeIgnored(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())
	defer r.Stop()

	// No EpisodeStarted -> should be a no-op, not a panic.
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r.TrackProgress(key, domain.TrackRef{}, 50)
	r.ByteProgress(key, 100, 200)
	r.SegmentProgress(key, 1, 2, 100, 200)
	r.HLSProgress(key, []domain.TrackProgressInfo{{Label: "x"}})

	r.mu.Lock()
	if len(r.currentEpisodes) != 0 {
		t.Errorf("no episodes should exist, got %d", len(r.currentEpisodes))
	}
	r.mu.Unlock()
}

func TestLiveReporter_TrackProgressClampsAndRecordsFirstProgress(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())
	defer r.Stop()

	key := domain.EpisodeKey{Season: 1, Episode: 1}
	r.EpisodeStarted(key)

	// Percent 1 (<2) should NOT set firstProgressAt.
	r.TrackProgress(key, domain.TrackRef{}, 1)
	r.mu.Lock()
	if !r.currentEpisodes[key].firstProgressAt.IsZero() {
		t.Errorf("firstProgressAt should be zero for pct<2")
	}
	r.mu.Unlock()

	// Percent over 100 should clamp to 100.
	r.TrackProgress(key, domain.TrackRef{}, 250)
	r.mu.Lock()
	ep := r.currentEpisodes[key]
	if ep.tracks[domain.TrackRef{}] != 100 {
		t.Errorf("percent should clamp to 100, got %d", ep.tracks[domain.TrackRef{}])
	}
	if ep.firstProgressAt.IsZero() {
		t.Errorf("firstProgressAt should be set after pct>=2")
	}
	if ep.firstProgressPct != 100 {
		t.Errorf("firstProgressPct=%d want 100", ep.firstProgressPct)
	}
	r.mu.Unlock()
}

func TestLiveReporter_DeferredAndFailed(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())
	defer r.Stop()

	key := domain.EpisodeKey{Season: 1, Episode: 2}
	r.EpisodeStarted(key)

	// Defer: removes from current/failed, adds to deferred.
	r.EpisodeDeferred(key, errors.New("net"), 1)
	r.mu.Lock()
	if _, ok := r.currentEpisodes[key]; ok {
		t.Errorf("deferred episode should leave currentEpisodes")
	}
	di, ok := r.deferredEpisodes[key]
	if !ok || di.attempts != 1 || di.err == nil {
		t.Errorf("deferred info wrong: %+v ok=%v", di, ok)
	}
	r.mu.Unlock()

	// Re-start clears deferred & failed markers.
	r.EpisodeStarted(key)
	r.mu.Lock()
	if _, ok := r.deferredEpisodes[key]; ok {
		t.Errorf("restart should clear deferred")
	}
	if _, ok := r.currentEpisodes[key]; !ok {
		t.Errorf("restart should re-register current episode")
	}
	r.mu.Unlock()

	// Fail: removes from current, adds to failed.
	r.EpisodeFailed(key, errors.New("fatal"))
	r.mu.Lock()
	if _, ok := r.currentEpisodes[key]; ok {
		t.Errorf("failed episode should leave currentEpisodes")
	}
	if r.failedEpisodes[key] == nil {
		t.Errorf("failed episode not recorded")
	}
	r.mu.Unlock()
}

func TestLiveReporter_RenderContainsKeyContent(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())

	key := domain.EpisodeKey{Series: "Test", Season: 1, Episode: 3}
	r.EpisodeStarted(key)
	r.TrackProgress(key, domain.TrackRef{Kind: domain.TrackVideo}, 50)
	r.HLSProgress(key, []domain.TrackProgressInfo{
		{Label: "Video", DoneSegments: 5, TotalSegments: 10},
	})
	r.Stop()

	out := buf.String()
	if !strings.Contains(out, "Series") {
		t.Errorf("missing Series row:\n%s", out)
	}
	if !strings.Contains(out, "2/10 episodes") {
		t.Errorf("missing series count:\n%s", out)
	}
	if !strings.Contains(out, "Season 1") || !strings.Contains(out, "Season 2") {
		t.Errorf("missing season rows:\n%s", out)
	}
	if !strings.Contains(out, "S01E03") {
		t.Errorf("missing episode row:\n%s", out)
	}
}

func TestLiveReporter_StopIsIdempotent(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())
	r.Stop()
	// Second Stop must not panic (ticker already stopped, done closed).
	r.Stop()
	r.mu.Lock()
	if !r.stopped {
		t.Errorf("should be stopped")
	}
	r.mu.Unlock()
}

func TestNewLive_Defaults(t *testing.T) {
	buf := &syncBuf{}
	coord := logx.NewCoordinator(buf)
	r := NewLive(buf, coord)
	if !r.isTTY {
		t.Errorf("NewLive should default isTTY=true")
	}
	if r.coord != coord {
		t.Errorf("coordinator not wired")
	}
	if r.w == nil {
		t.Errorf("writer not wired")
	}
}

// TestLiveReporter_LogCoordination drives the clear/redraw callbacks that the
// coordinator invokes around a log line, exercising clearForLog and redraw.
func TestLiveReporter_LogCoordination(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())
	defer r.Stop()

	// A log line through the coordinator should call clearForLog then redraw.
	r.coord.WriteLog("hello from log")

	out := buf.String()
	if !strings.Contains(out, "hello from log") {
		t.Errorf("log line not written through coordinator:\n%s", out)
	}
	// The progress frame should be repainted after the log (Series row present).
	if !strings.Contains(out, "Series") {
		t.Errorf("progress not repainted after log:\n%s", out)
	}
}

func TestLiveReporter_RenderFailedAndDeferredSections(t *testing.T) {
	buf := &syncBuf{}
	r := newPlainLive(buf)
	r.Start(samplePlan())

	r.EpisodeFailed(domain.EpisodeKey{Season: 2, Episode: 1}, errors.New("download error xyz"))
	r.EpisodeDeferred(domain.EpisodeKey{Season: 1, Episode: 5}, errors.New("timeout"), 3)
	r.Stop()

	out := buf.String()
	if !strings.Contains(out, "S02E01") {
		t.Errorf("missing failed episode label:\n%s", out)
	}
	if !strings.Contains(out, "download error xyz") {
		t.Errorf("missing failed error text:\n%s", out)
	}
	if !strings.Contains(out, "S01E05") {
		t.Errorf("missing deferred episode label:\n%s", out)
	}
	if !strings.Contains(out, "attempt 3") {
		t.Errorf("missing deferred attempt count:\n%s", out)
	}
}
