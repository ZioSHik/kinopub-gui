package termuxapi

import (
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// capturedCmd records one runCmd invocation.
type capturedCmd struct {
	name string
	args []string
}

// recorder is a thread-safe capture of runCmd invocations, used to inspect the
// exact termux-* command name and argument vector the Notifier constructs.
type recorder struct {
	mu    sync.Mutex
	calls []capturedCmd
}

func (r *recorder) run(name string, args ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// copy args so later mutation of the caller's slice can't corrupt history
	cp := append([]string(nil), args...)
	r.calls = append(r.calls, capturedCmd{name: name, args: cp})
}

func (r *recorder) snapshot() []capturedCmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capturedCmd(nil), r.calls...)
}

// installRecorder swaps runCmd for a recorder for the duration of the test.
func installRecorder(t *testing.T) *recorder {
	t.Helper()
	rec := &recorder{}
	orig := runCmd
	runCmd = rec.run
	t.Cleanup(func() { runCmd = orig })
	return rec
}

// forceAvailable overrides lookPath so Available() returns the desired result.
func forceAvailable(t *testing.T, ok bool) {
	t.Helper()
	orig := lookPath
	lookPath = func(string) (string, error) {
		if ok {
			return "/data/data/com.termux/files/usr/bin/termux-notification", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = orig })
}

// fakeReporter is an in-memory domain.ProgressReporter that records the calls
// forwarded to inner. It implements all optional sink interfaces.
type fakeReporter struct {
	mu              sync.Mutex
	started         []domain.SeriesPlan
	epStarted       []domain.EpisodeKey
	trackProgress   []trackCall
	epCompleted     []domain.EpisodeKey
	epFailed        []failCall
	stopped         int
	hlsProgress     []hlsCall
	segmentProgress []segCall
	byteProgress    []byteCall
}

type trackCall struct {
	key     domain.EpisodeKey
	track   domain.TrackRef
	percent int
}
type failCall struct {
	key domain.EpisodeKey
	err error
}
type hlsCall struct {
	key    domain.EpisodeKey
	tracks []domain.TrackProgressInfo
}
type segCall struct {
	key                     domain.EpisodeKey
	done, total             int
	downloaded, approxTotal int64
}
type byteCall struct {
	key               domain.EpisodeKey
	downloaded, total int64
}

func (f *fakeReporter) Start(plan domain.SeriesPlan) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, plan)
}
func (f *fakeReporter) EpisodeStarted(key domain.EpisodeKey) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.epStarted = append(f.epStarted, key)
}
func (f *fakeReporter) TrackProgress(key domain.EpisodeKey, track domain.TrackRef, percent int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trackProgress = append(f.trackProgress, trackCall{key, track, percent})
}
func (f *fakeReporter) EpisodeCompleted(key domain.EpisodeKey) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.epCompleted = append(f.epCompleted, key)
}
func (f *fakeReporter) EpisodeFailed(key domain.EpisodeKey, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.epFailed = append(f.epFailed, failCall{key, err})
}
func (f *fakeReporter) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped++
}
func (f *fakeReporter) HLSProgress(key domain.EpisodeKey, tracks []domain.TrackProgressInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hlsProgress = append(f.hlsProgress, hlsCall{key, tracks})
}
func (f *fakeReporter) SegmentProgress(key domain.EpisodeKey, done, total int, downloaded, approxTotal int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.segmentProgress = append(f.segmentProgress, segCall{key, done, total, downloaded, approxTotal})
}
func (f *fakeReporter) ByteProgress(key domain.EpisodeKey, downloaded, total int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byteProgress = append(f.byteProgress, byteCall{key, downloaded, total})
}

// onlyRequired implements ONLY domain.ProgressReporter and none of the optional
// sink interfaces, to exercise the type-assertion no-op paths in the Notifier.
type onlyRequired struct {
	r *fakeReporter
}

func (o onlyRequired) Start(p domain.SeriesPlan)          { o.r.Start(p) }
func (o onlyRequired) EpisodeStarted(k domain.EpisodeKey) { o.r.EpisodeStarted(k) }
func (o onlyRequired) TrackProgress(k domain.EpisodeKey, t domain.TrackRef, p int) {
	o.r.TrackProgress(k, t, p)
}
func (o onlyRequired) EpisodeCompleted(k domain.EpisodeKey)       { o.r.EpisodeCompleted(k) }
func (o onlyRequired) EpisodeFailed(k domain.EpisodeKey, e error) { o.r.EpisodeFailed(k, e) }
func (o onlyRequired) Stop()                                      { o.r.Stop() }

// waitForNotifyDone blocks until refresh()'s background goroutine has finished
// (the throttle flag returns to false), keeping the test deterministic without
// real sleeps. Fails the test if it does not settle promptly.
func waitForNotifyDone(t *testing.T, n *Notifier) {
	t.Helper()
	for i := 0; i < 10000; i++ {
		if !n.notifying.Load() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("notify goroutine did not finish")
}

// arg returns the value following the named flag in a captured arg vector.
func arg(t *testing.T, c capturedCmd, flag string) string {
	t.Helper()
	for i := 0; i < len(c.args)-1; i++ {
		if c.args[i] == flag {
			return c.args[i+1]
		}
	}
	t.Fatalf("flag %q not found in args %v", flag, c.args)
	return ""
}

func hasFlag(c capturedCmd, flag string) bool {
	for _, a := range c.args {
		if a == flag {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Available
// ---------------------------------------------------------------------------

func TestAvailable(t *testing.T) {
	forceAvailable(t, true)
	if !Available() {
		t.Fatal("Available() = false; want true when termux-notification is on PATH")
	}
	forceAvailable(t, false)
	if Available() {
		t.Fatal("Available() = true; want false when termux-notification is absent")
	}
}

// ---------------------------------------------------------------------------
// Wrap
// ---------------------------------------------------------------------------

func TestWrapReturnsInnerWhenUnavailable(t *testing.T) {
	forceAvailable(t, false)
	inner := &fakeReporter{}
	got := Wrap(inner)
	if got != domain.ProgressReporter(inner) {
		t.Fatalf("Wrap returned %T; want the unwrapped inner reporter when Termux:API is absent", got)
	}
}

func TestWrapWrapsWhenAvailable(t *testing.T) {
	forceAvailable(t, true)
	inner := &fakeReporter{}
	got := Wrap(inner)
	if _, ok := got.(*Notifier); !ok {
		t.Fatalf("Wrap returned %T; want *Notifier when Termux:API is available", got)
	}
	if got == domain.ProgressReporter(inner) {
		t.Fatal("Wrap returned inner unchanged; want a wrapping Notifier")
	}
}

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------

func TestStart_ForwardsAndNotifies(t *testing.T) {
	rec := installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}

	plan := domain.SeriesPlan{Title: "Лучшие", Total: 12}
	n.Start(plan)

	// forwarded to inner exactly once with the same plan
	if len(inner.started) != 1 || !reflect.DeepEqual(inner.started[0], plan) {
		t.Fatalf("inner.Start not forwarded correctly: %+v", inner.started)
	}
	// internal state recorded
	if n.total != 12 || n.seriesTitle != "Лучшие" {
		t.Fatalf("state not stored: total=%d title=%q", n.total, n.seriesTitle)
	}

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 termux call, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.name != "termux-notification" {
		t.Fatalf("cmd name = %q", c.name)
	}
	if got := arg(t, c, "--id"); got != notificationID {
		t.Fatalf("--id = %q want %q", got, notificationID)
	}
	if got := arg(t, c, "--title"); got != "kinopub — Лучшие" {
		t.Fatalf("--title = %q", got)
	}
	if got := arg(t, c, "--content"); got != "Начало загрузки · 12 эп." {
		t.Fatalf("--content = %q", got)
	}
	if got := arg(t, c, "--progress"); got != "0" {
		t.Fatalf("--progress = %q want 0", got)
	}
	if !hasFlag(c, "--ongoing") || !hasFlag(c, "--priority") {
		t.Fatalf("missing ongoing/priority flags: %v", c.args)
	}
}

// ---------------------------------------------------------------------------
// EpisodeStarted / TrackProgress forwarding + refresh
// ---------------------------------------------------------------------------

func TestEpisodeStarted_StoresLabelAndForwards(t *testing.T) {
	installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}

	key := domain.EpisodeKey{Season: 3, Episode: 7}
	n.EpisodeStarted(key)
	waitForNotifyDone(t, n)

	if got, _ := n.currentEp.Load().(string); got != "S03E07" {
		t.Fatalf("currentEp = %q want S03E07", got)
	}
	if n.currentPct.Load() != 0 {
		t.Fatalf("currentPct = %d want 0 after EpisodeStarted", n.currentPct.Load())
	}
	if len(inner.epStarted) != 1 || inner.epStarted[0] != key {
		t.Fatalf("EpisodeStarted not forwarded: %+v", inner.epStarted)
	}
}

func TestTrackProgress_StoresPctAndForwards(t *testing.T) {
	installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}

	key := domain.EpisodeKey{Season: 1, Episode: 2}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}
	n.TrackProgress(key, track, 63)
	waitForNotifyDone(t, n)

	if n.currentPct.Load() != 63 {
		t.Fatalf("currentPct = %d want 63", n.currentPct.Load())
	}
	if len(inner.trackProgress) != 1 || inner.trackProgress[0] != (trackCall{key, track, 63}) {
		t.Fatalf("TrackProgress not forwarded: %+v", inner.trackProgress)
	}
}

func TestRefresh_SeriesPercentMath(t *testing.T) {
	rec := installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}

	// total=10, completed=2, current episode at 50%.
	n.total = 10
	n.completed = 2
	n.seriesTitle = "Шоу"
	n.currentEp.Store("S01E03")
	n.currentPct.Store(50)

	n.refresh()
	// refresh fires the notification in a goroutine; wait for the throttle flag
	// to clear so we know it ran.
	waitForNotifyDone(t, n)

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	// seriesPct = done*100/total + pct/total = 2*100/10 + 50/10 = 20 + 5 = 25
	if got := arg(t, c, "--progress"); got != "25" {
		t.Fatalf("--progress = %q want 25", got)
	}
	if got := arg(t, c, "--title"); got != "kinopub ↓ 25% — Шоу" {
		t.Fatalf("--title = %q", got)
	}
	if got := arg(t, c, "--content"); got != "S01E03 · 2/10 эп.  50%" {
		t.Fatalf("--content = %q", got)
	}
}

func TestRefresh_ZeroTotalNoDivideByZero(t *testing.T) {
	rec := installRecorder(t)
	n := &Notifier{inner: &fakeReporter{}}
	n.total = 0
	n.completed = 0
	n.currentEp.Store("S01E01")
	n.currentPct.Store(99)

	n.refresh()
	waitForNotifyDone(t, n)

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(calls))
	}
	if got := arg(t, calls[0], "--progress"); got != "0" {
		t.Fatalf("--progress = %q want 0 when total=0", got)
	}
}

func TestRefresh_ThrottleSkipsWhileInFlight(t *testing.T) {
	rec := installRecorder(t)
	n := &Notifier{inner: &fakeReporter{}}
	n.total = 4
	n.currentEp.Store("S01E01")

	// Simulate an in-flight notification: set the throttle flag manually.
	if !n.notifying.CompareAndSwap(false, true) {
		t.Fatal("precondition: throttle should start clear")
	}
	n.refresh() // must be a no-op because notifying is already true
	if len(rec.snapshot()) != 0 {
		t.Fatalf("refresh issued a command while one was in flight: %+v", rec.snapshot())
	}
	n.notifying.Store(false)
}

// ---------------------------------------------------------------------------
// EpisodeCompleted
// ---------------------------------------------------------------------------

func TestEpisodeCompleted_IncrementsAndNotifies(t *testing.T) {
	rec := installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}
	n.total = 4
	n.seriesTitle = "Сериал"

	key := domain.EpisodeKey{Season: 2, Episode: 5}
	n.EpisodeCompleted(key)

	if n.completed != 1 {
		t.Fatalf("completed = %d want 1", n.completed)
	}
	if len(inner.epCompleted) != 1 || inner.epCompleted[0] != key {
		t.Fatalf("EpisodeCompleted not forwarded")
	}
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(calls))
	}
	c := calls[0]
	// pct = 1*100/4 = 25
	if got := arg(t, c, "--title"); got != "kinopub 25% — Сериал" {
		t.Fatalf("--title = %q", got)
	}
	if got := arg(t, c, "--content"); got != "S02E05 готово · 1/4 эп." {
		t.Fatalf("--content = %q", got)
	}
	if got := arg(t, c, "--progress"); got != "25" {
		t.Fatalf("--progress = %q want 25", got)
	}
}

func TestEpisodeCompleted_ZeroTotalPctZero(t *testing.T) {
	rec := installRecorder(t)
	n := &Notifier{inner: &fakeReporter{}}
	n.total = 0
	n.seriesTitle = "X"

	n.EpisodeCompleted(domain.EpisodeKey{Season: 1, Episode: 1})
	c := rec.snapshot()[0]
	if got := arg(t, c, "--progress"); got != "0" {
		t.Fatalf("--progress = %q want 0 when total=0", got)
	}
	if got := arg(t, c, "--title"); got != "kinopub 0% — X" {
		t.Fatalf("--title = %q", got)
	}
}

func TestEpisodeCompleted_FullSeries100Pct(t *testing.T) {
	rec := installRecorder(t)
	n := &Notifier{inner: &fakeReporter{}}
	n.total = 2
	n.completed = 1
	n.seriesTitle = "Done"

	n.EpisodeCompleted(domain.EpisodeKey{Season: 1, Episode: 2})
	c := rec.snapshot()[0]
	if got := arg(t, c, "--progress"); got != "100" {
		t.Fatalf("--progress = %q want 100", got)
	}
}

// ---------------------------------------------------------------------------
// EpisodeFailed
// ---------------------------------------------------------------------------

func TestEpisodeFailed_ForwardsNoNotification(t *testing.T) {
	rec := installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}

	key := domain.EpisodeKey{Season: 1, Episode: 9}
	wantErr := errors.New("boom")
	n.EpisodeFailed(key, wantErr)

	if len(inner.epFailed) != 1 || inner.epFailed[0].key != key || inner.epFailed[0].err != wantErr {
		t.Fatalf("EpisodeFailed not forwarded: %+v", inner.epFailed)
	}
	if len(rec.snapshot()) != 0 {
		t.Fatalf("EpisodeFailed should not emit a notification, got %+v", rec.snapshot())
	}
}

// ---------------------------------------------------------------------------
// Stop
// ---------------------------------------------------------------------------

func TestStop_SuccessShowsDoneAndVibrates(t *testing.T) {
	rec := installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}
	n.total = 3
	n.completed = 3
	n.seriesTitle = "Финал"

	n.Stop()

	if inner.stopped != 1 {
		t.Fatalf("inner.Stop not forwarded (count=%d)", inner.stopped)
	}
	calls := rec.snapshot()
	if len(calls) != 2 {
		t.Fatalf("want 2 calls (notification + vibrate), got %d: %+v", len(calls), calls)
	}
	notif := calls[0]
	if notif.name != "termux-notification" {
		t.Fatalf("first cmd = %q", notif.name)
	}
	if got := arg(t, notif, "--title"); got != "✓ Финал" {
		t.Fatalf("--title = %q", got)
	}
	if got := arg(t, notif, "--content"); got != "Скачано 3 эпизодов" {
		t.Fatalf("--content = %q", got)
	}
	// The completion notification is final, not --ongoing.
	if hasFlag(notif, "--ongoing") {
		t.Fatalf("completion notification should not be --ongoing: %v", notif.args)
	}
	vib := calls[1]
	if vib.name != "termux-vibrate" || !reflect.DeepEqual(vib.args, []string{"-d", "400"}) {
		t.Fatalf("vibrate call wrong: %+v", vib)
	}
}

func TestStop_IncompleteRemovesNotification(t *testing.T) {
	rec := installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}
	n.total = 5
	n.completed = 2 // partial
	n.seriesTitle = "Прервано"

	n.Stop()

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 call (remove), got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.name != "termux-notification-remove" {
		t.Fatalf("cmd = %q want termux-notification-remove", c.name)
	}
	if !reflect.DeepEqual(c.args, []string{notificationID}) {
		t.Fatalf("remove args = %v want [%s]", c.args, notificationID)
	}
}

func TestStop_NothingCompletedRemoves(t *testing.T) {
	rec := installRecorder(t)
	n := &Notifier{inner: &fakeReporter{}}
	n.total = 5
	n.completed = 0

	n.Stop()
	calls := rec.snapshot()
	if len(calls) != 1 || calls[0].name != "termux-notification-remove" {
		t.Fatalf("want a single remove call, got %+v", calls)
	}
}

// ---------------------------------------------------------------------------
// Optional sink forwarding
// ---------------------------------------------------------------------------

func TestSinkForwarding_WhenInnerImplements(t *testing.T) {
	installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}

	key := domain.EpisodeKey{Season: 1, Episode: 1}
	tracks := []domain.TrackProgressInfo{{Label: "Video", DoneSegments: 3, TotalSegments: 10}}
	n.HLSProgress(key, tracks)
	n.SegmentProgress(key, 4, 8, 1000, 2000)
	n.ByteProgress(key, 500, 1000)

	if len(inner.hlsProgress) != 1 || !reflect.DeepEqual(inner.hlsProgress[0], hlsCall{key, tracks}) {
		t.Fatalf("HLSProgress not forwarded: %+v", inner.hlsProgress)
	}
	if len(inner.segmentProgress) != 1 || inner.segmentProgress[0] != (segCall{key, 4, 8, 1000, 2000}) {
		t.Fatalf("SegmentProgress not forwarded: %+v", inner.segmentProgress)
	}
	if len(inner.byteProgress) != 1 || inner.byteProgress[0] != (byteCall{key, 500, 1000}) {
		t.Fatalf("ByteProgress not forwarded: %+v", inner.byteProgress)
	}
}

func TestSinkForwarding_WhenInnerLacksOptional_NoPanic(t *testing.T) {
	installRecorder(t)
	base := &fakeReporter{}
	inner := onlyRequired{r: base} // implements only ProgressReporter
	n := &Notifier{inner: inner}

	key := domain.EpisodeKey{Season: 1, Episode: 1}
	// These must be safe no-ops (type assertions fail) and not panic.
	n.HLSProgress(key, []domain.TrackProgressInfo{{Label: "Video"}})
	n.SegmentProgress(key, 1, 2, 10, 20)
	n.ByteProgress(key, 10, 20)

	if len(base.hlsProgress)+len(base.segmentProgress)+len(base.byteProgress) != 0 {
		t.Fatal("optional sink calls should have been dropped when inner does not implement them")
	}
}

// ---------------------------------------------------------------------------
// End-to-end lifecycle through the Notifier
// ---------------------------------------------------------------------------

func TestLifecycle_StartProgressCompleteStop(t *testing.T) {
	rec := installRecorder(t)
	inner := &fakeReporter{}
	n := &Notifier{inner: inner}

	n.Start(domain.SeriesPlan{Title: "Жизнь", Total: 2})
	k1 := domain.EpisodeKey{Season: 1, Episode: 1}
	k2 := domain.EpisodeKey{Season: 1, Episode: 2}
	n.EpisodeStarted(k1)
	waitForNotifyDone(t, n)
	n.TrackProgress(k1, domain.TrackRef{}, 100)
	waitForNotifyDone(t, n)
	n.EpisodeCompleted(k1)
	n.EpisodeStarted(k2)
	waitForNotifyDone(t, n)
	n.TrackProgress(k2, domain.TrackRef{}, 100)
	waitForNotifyDone(t, n)
	n.EpisodeCompleted(k2)
	n.Stop()

	if n.completed != 2 {
		t.Fatalf("completed = %d want 2", n.completed)
	}
	// last calls must include the success notification + vibrate.
	calls := rec.snapshot()
	last := calls[len(calls)-1]
	if last.name != "termux-vibrate" {
		t.Fatalf("final call = %q want termux-vibrate (success path)", last.name)
	}
	prev := calls[len(calls)-2]
	if got := arg(t, prev, "--title"); got != "✓ Жизнь" {
		t.Fatalf("success title = %q", got)
	}
}
