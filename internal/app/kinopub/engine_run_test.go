package kinopub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// ---------------------------------------------------------------------------
// run() entry-point guards
// ---------------------------------------------------------------------------

func TestRun_MissingDownloaderDeps(t *testing.T) {
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	// run() requires both HLSDownloader and PageScraper.
	e.deps.HLSDownloader = nil
	_, err := e.run(context.Background(), retryTestConfig())
	if err == nil || err.Error() != "downloader not configured" {
		t.Fatalf("want 'downloader not configured', got %v", err)
	}

	e2, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	e2.deps.PageScraper = nil
	if _, err := e2.run(context.Background(), retryTestConfig()); err == nil {
		t.Fatal("nil PageScraper should error")
	}
}

func TestRun_EmptyInputURL(t *testing.T) {
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	cfg := retryTestConfig()
	cfg.InputURL = ""
	_, err := e.run(context.Background(), cfg)
	if err == nil || err.Error() != "a kino.pub URL is required" {
		t.Fatalf("want URL-required error, got %v", err)
	}
}

func TestRun_HappyPathDelegatesToHLS(t *testing.T) {
	hls := newFakeHLS(nil)
	e, _, ss := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(2)})
	res, err := e.run(context.Background(), retryTestConfig())
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if res.Total != 2 || res.Succeeded != 2 {
		t.Fatalf("Total/Succeeded = %d/%d, want 2/2", res.Total, res.Succeeded)
	}
	if !ss.completed[domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}] {
		t.Error("episode 1 not completed")
	}
}

// ---------------------------------------------------------------------------
// Scrape failure / retry behaviour
// ---------------------------------------------------------------------------

// flakyScraper fails the first failUntil-1 calls, then returns the playlist.
type flakyScraper struct {
	mu        sync.Mutex
	calls     int
	failUntil int
	err       error
	playlist  *domain.PagePlaylist
}

func (f *flakyScraper) ExtractAllSeasons(context.Context, string) (*domain.PagePlaylist, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls < f.failUntil {
		return nil, f.err
	}
	return f.playlist, nil
}

// To exercise the scrape retry loop, fail exactly once (one 2s real backoff).
func TestRunHLS_ScrapeRetryThenSuccessFast(t *testing.T) {
	if testing.Short() {
		t.Skip("uses a real 2s backoff sleep")
	}
	scraper := &flakyScraper{
		failUntil: 2, // fail once then succeed
		err:       errors.New("temporary cloudflare 503"),
		playlist:  makePlaylist(1),
	}
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), scraper)
	res, err := e.runHLS(context.Background(), retryTestConfig())
	if err != nil {
		t.Fatalf("runHLS error after one transient scrape failure: %v", err)
	}
	if res.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", res.Succeeded)
	}
	if scraper.calls != 2 {
		t.Errorf("scraper called %d times, want 2", scraper.calls)
	}
}

func TestRunHLS_ScrapeRetryAbortsOnContextCancel(t *testing.T) {
	scraper := &flakyScraper{
		failUntil: 99, // always fail
		err:       errors.New("503 service unavailable"),
		playlist:  makePlaylist(1),
	}
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), scraper)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled → first scrape failure path checks ctx.Err()
	_, err := e.runHLS(ctx, retryTestConfig())
	if err == nil {
		t.Fatal("expected error when context canceled during scrape")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled wrapped, got %v", err)
	}
}

func TestRunHLS_NoEpisodesError(t *testing.T) {
	pl := &domain.PagePlaylist{ItemID: 42, Title: "Empty"} // no episodes
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: pl})
	_, err := e.runHLS(context.Background(), retryTestConfig())
	if err == nil || err.Error() != "no episodes found in page playlist" {
		t.Fatalf("want 'no episodes found in page playlist', got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Dry run, all-completed, skipped-no-manifest
// ---------------------------------------------------------------------------

func TestRunHLS_DryRunListsWithoutDownloading(t *testing.T) {
	hls := newFakeHLS(nil)
	e, rec, ss := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(3)})
	cfg := retryTestConfig()
	cfg.DryRun = true
	res, err := e.runHLS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dry run error: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("dry run Total = %d, want 3", res.Total)
	}
	if res.Succeeded != 0 {
		t.Errorf("dry run Succeeded = %d, want 0", res.Succeeded)
	}
	if len(hls.calls) != 0 {
		t.Errorf("dry run must not download, got %d episode calls", len(hls.calls))
	}
	if len(ss.completed) != 0 {
		t.Errorf("dry run must not mark completed, got %v", ss.completed)
	}
	if rec.started != nil {
		t.Errorf("dry run must not start progress, got %v", rec.started)
	}
}

// completedStateStore marks specific episodes as already completed.
type completedStateStore struct {
	mockStateStore
	done map[domain.EpisodeKey]bool
}

func (c *completedStateStore) IsCompleted(_ domain.DownloadState, key domain.EpisodeKey) bool {
	return c.done[key]
}

func TestRunHLS_AllCompletedReturnsZero(t *testing.T) {
	hls := newFakeHLS(nil)
	e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(2)})
	css := &completedStateStore{done: map[domain.EpisodeKey]bool{
		{Series: "42", Season: 1, Episode: 1}: true,
		{Series: "42", Season: 1, Episode: 2}: true,
	}}
	e.deps.StateStore = css
	res, err := e.runHLS(context.Background(), retryTestConfig())
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("Total = %d, want 0 (all completed)", res.Total)
	}
	if len(hls.calls) != 0 {
		t.Errorf("should not download anything, got %d calls", len(hls.calls))
	}
}

func TestRunHLS_ForceRedownloadIgnoresCompleted(t *testing.T) {
	hls := newFakeHLS(nil)
	e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(2)})
	css := &completedStateStore{done: map[domain.EpisodeKey]bool{
		{Series: "42", Season: 1, Episode: 1}: true,
	}}
	e.deps.StateStore = css
	cfg := retryTestConfig()
	cfg.ForceRedownload = true
	res, err := e.runHLS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Succeeded != 2 {
		t.Errorf("ForceRedownload should re-download all, Succeeded=%d want 2", res.Succeeded)
	}
}

// missingManifestScraper returns a playlist where one episode has no manifest URL.
func TestRunHLS_SkipsEpisodeWithoutManifest(t *testing.T) {
	pl := makePlaylist(2)
	pl.Episodes[1].ManifestURL = "" // E02 has no manifest → skipped
	hls := newFakeHLS(nil)
	e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: pl})
	res, err := e.runHLS(context.Background(), retryTestConfig())
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (no-manifest episode)", res.Skipped)
	}
	if res.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", res.Succeeded)
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2 (selected before skip)", res.Total)
	}
}

// ---------------------------------------------------------------------------
// RetryOnly with non-matching key → nothing to download
// ---------------------------------------------------------------------------

func TestRunHLS_RetryOnlyNoMatchDownloadsNothing(t *testing.T) {
	hls := newFakeHLS(nil)
	e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(2)})
	cfg := retryTestConfig()
	cfg.RetryOnly = []domain.EpisodeKey{{Season: 9, Episode: 9}} // no such episode
	res, err := e.runHLS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("Total = %d, want 0 (no episode matched RetryOnly)", res.Total)
	}
	if len(hls.calls) != 0 {
		t.Errorf("nothing should download, got %d calls", len(hls.calls))
	}
}

// ---------------------------------------------------------------------------
// Fatal error path inside attemptHLSEpisode (output path / dir errors)
// ---------------------------------------------------------------------------

func TestAttemptHLSEpisode_OutputPathError(t *testing.T) {
	e, rec, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.OutputLayout = &mockOutputLayout{err: errors.New("disk full")}
	cfg := retryTestConfig()
	res, err := e.runHLS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runHLS returned engine error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("Failed = %d, want 1 (output path error is fatal)", res.Failed)
	}
	if len(rec.failed) != 1 {
		t.Errorf("EpisodeFailed reported %d times, want 1", len(rec.failed))
	}
}

// muxFailDownloader satisfies HLSMuxer but always fails muxing (fatal).
type muxFailDownloader struct {
	mockDownloader
}

func (m *muxFailDownloader) MuxHLS(context.Context, domain.Job, *domain.HLSDownloadResult) error {
	return errors.New("ffmpeg exited 1")
}

func TestAttemptHLSEpisode_MuxFailureIsFatal(t *testing.T) {
	e, rec, ss := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.Downloader = &muxFailDownloader{}
	res, err := e.runHLS(context.Background(), retryTestConfig())
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("Failed = %d, want 1 (mux failure is fatal)", res.Failed)
	}
	if len(rec.failed) != 1 {
		t.Errorf("EpisodeFailed reported %d times, want 1", len(rec.failed))
	}
	if len(ss.completed) != 0 {
		t.Errorf("mux failure must not mark completed, got %v", ss.completed)
	}
}

// noMuxDownloader is a plain Downloader that does NOT implement HLSMuxer, so the
// engine's "downloader does not support HLS muxing" fatal path is hit.
type noMuxDownloader struct{ mockDownloader }

func TestAttemptHLSEpisode_NoMuxerIsFatal(t *testing.T) {
	e, rec, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.Downloader = &noMuxDownloader{}
	res, err := e.runHLS(context.Background(), retryTestConfig())
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("Failed = %d, want 1 (no muxer support)", res.Failed)
	}
	if len(rec.failed) != 1 {
		t.Errorf("EpisodeFailed reported %d times, want 1", len(rec.failed))
	}
}

// ---------------------------------------------------------------------------
// Whole-run cancel returns ctx error and counts failures (non-paused)
// ---------------------------------------------------------------------------

func TestRunHLS_CanceledRunReturnsContextError(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	g := newGatedHLS(k1)
	e, _, _ := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(2)})
	// No Paused() set → a cancel is a hard stop (failures counted).
	ctx, cancel := context.WithCancel(context.Background())
	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1

	done := make(chan struct {
		res domain.RunResult
		err error
	}, 1)
	go func() {
		res, err := e.runHLS(ctx, cfg)
		done <- struct {
			res domain.RunResult
			err error
		}{res, err}
	}()

	select {
	case <-g.started[k1]:
	case <-time.After(2 * time.Second):
		t.Fatal("episode 1 never started")
	}
	cancel() // E01's gate returns ctx.Err() (retryable under ctx cancel → re-parked)

	select {
	case got := <-done:
		if got.err == nil || !errors.Is(got.err, context.Canceled) {
			t.Errorf("want context.Canceled, got %v", got.err)
		}
		// E01 re-parked + E02 never started → both swept as failures (not paused).
		if got.res.Failed == 0 {
			t.Errorf("canceled (non-paused) run should count failures, got Failed=%d", got.res.Failed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish after cancel")
	}
}

// ---------------------------------------------------------------------------
// Prioritize requests move an episode to the front
// ---------------------------------------------------------------------------

func TestRunHLS_PrioritizeMovesEpisodeToFront(t *testing.T) {
	k1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	k2 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 2}
	k3 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 3}
	g := newGatedHLS(k1, k2, k3)
	e, _, _ := newRetryTestEngine(g, &fakePageScraper{playlist: makePlaylist(3)})

	prio := make(chan domain.EpisodeKey, 4)
	prio <- k3 // ask E03 to jump ahead of E02 before the run begins
	e.deps.PrioritizeRequests = prio

	cfg := retryTestConfig()
	cfg.MaxConcurrency = 1
	done := make(chan domain.RunResult, 1)
	go func() {
		res, _ := e.runHLS(context.Background(), cfg)
		done <- res
	}()

	// E01 runs first (it was already at the head when the request was drained,
	// but moveToFront put E03 ahead of E02). Release in arrival order and record.
	order := []domain.EpisodeKey{}
	for i := 0; i < 3; i++ {
		select {
		case <-g.started[k1]:
			order = append(order, k1)
			close(g.release[k1])
		case <-g.started[k2]:
			order = append(order, k2)
			close(g.release[k2])
		case <-g.started[k3]:
			order = append(order, k3)
			close(g.release[k3])
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d episodes started: %v", len(order), order)
		}
	}
	<-done
	// E03 must come before E02 in the dispatch order.
	idx3, idx2 := -1, -1
	for i, k := range order {
		if k == k3 {
			idx3 = i
		}
		if k == k2 {
			idx2 = i
		}
	}
	if idx3 > idx2 {
		t.Errorf("prioritized E03 (idx %d) should run before E02 (idx %d): order=%v", idx3, idx2, order)
	}
}

// ---------------------------------------------------------------------------
// selectEpisodes wrapper
// ---------------------------------------------------------------------------

func TestSelectEpisodes_FiltersCompleted(t *testing.T) {
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(3)})
	css := &completedStateStore{done: map[domain.EpisodeKey]bool{
		{Series: "1", Season: 1, Episode: 1}: true,
	}}
	e.deps.StateStore = css
	series := sampleSeries()
	cfg := domain.RunConfig{SeasonSel: domain.Selection{All: true}, EpisodeSel: domain.Selection{All: true}}
	got := e.selectEpisodes(series, domain.DownloadState{}, cfg)
	// sampleSeries has 3 episodes; S1E1 is completed → 2 remain.
	if len(got) != 2 {
		t.Fatalf("selectEpisodes = %d, want 2 (one completed filtered)", len(got))
	}
	for _, ep := range got {
		if ep.Key.Season == 1 && ep.Key.Episode == 1 {
			t.Errorf("completed S1E1 should have been filtered out")
		}
	}
}

// ---------------------------------------------------------------------------
// matchingEpisodes with season/episode selection ranges
// ---------------------------------------------------------------------------

func TestMatchingEpisodes_SeasonAndEpisodeSelection(t *testing.T) {
	e := &engine{}
	series := sampleSeries() // S1E1,S1E2,S2E1
	cfg := domain.RunConfig{
		SeasonSel:  domain.Selection{Values: map[int]bool{1: true}},
		EpisodeSel: domain.Selection{Values: map[int]bool{2: true}},
	}
	got := e.matchingEpisodes(series, cfg)
	if len(got) != 1 || got[0].Key.Season != 1 || got[0].Key.Episode != 2 {
		t.Fatalf("season=1 episode=2 selection = %+v, want only S1E2", got)
	}
}

// ---------------------------------------------------------------------------
// countCompletedPerSeason
// ---------------------------------------------------------------------------

func TestCountCompletedPerSeason(t *testing.T) {
	eps := []domain.Episode{
		{Key: domain.EpisodeKey{Season: 1, Episode: 1}},
		{Key: domain.EpisodeKey{Season: 1, Episode: 2}},
		{Key: domain.EpisodeKey{Season: 2, Episode: 1}},
	}
	store := &completedStateStore{done: map[domain.EpisodeKey]bool{
		{Season: 1, Episode: 1}: true,
		{Season: 2, Episode: 1}: true,
	}}
	got := countCompletedPerSeason(eps, domain.DownloadState{}, store)
	if got[1] != 1 || got[2] != 1 {
		t.Fatalf("countCompletedPerSeason = %v, want {1:1, 2:1}", got)
	}
}

// ---------------------------------------------------------------------------
// downloadExecutor.Execute
// ---------------------------------------------------------------------------

// recordingExecDownloader records the job it was asked to download.
type recordingExecDownloader struct {
	mu  sync.Mutex
	job domain.Job
	err error
}

func (r *recordingExecDownloader) Download(_ context.Context, job domain.Job, _ domain.ProgressSink) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.job = job
	return r.err
}

func TestDownloadExecutor_Execute(t *testing.T) {
	dl := &recordingExecDownloader{}
	rec := &recordingReporter{}
	exec := &downloadExecutor{downloader: dl, reporter: rec}
	key := domain.EpisodeKey{Season: 3, Episode: 4}
	job := domain.Job{Episode: domain.Episode{Key: key}}
	if err := exec.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(rec.started) != 1 || rec.started[0] != key {
		t.Errorf("EpisodeStarted not reported for %v, got %v", key, rec.started)
	}
	if dl.job.Episode.Key != key {
		t.Errorf("downloader received wrong job: %v", dl.job.Episode.Key)
	}

	dl2 := &recordingExecDownloader{err: errors.New("boom")}
	exec2 := &downloadExecutor{downloader: dl2, reporter: &recordingReporter{}}
	if err := exec2.Execute(context.Background(), job); err == nil {
		t.Error("Execute should propagate downloader error")
	}
}

// ---------------------------------------------------------------------------
// downloadPoster
// ---------------------------------------------------------------------------

func TestDownloadPoster_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("JPEGDATA"))
	}))
	defer srv.Close()

	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	dir := t.TempDir()
	path, err := e.downloadPoster(context.Background(), srv.URL, dir)
	if err != nil {
		t.Fatalf("downloadPoster error: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("poster path %q not under %q", path, dir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read poster: %v", err)
	}
	if string(data) != "JPEGDATA" {
		t.Errorf("poster content = %q, want JPEGDATA", data)
	}
}

func TestDownloadPoster_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	_, err := e.downloadPoster(context.Background(), srv.URL, t.TempDir())
	if err == nil {
		t.Fatal("expected error on HTTP 404")
	}
}

func TestDownloadPoster_BadURL(t *testing.T) {
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	// Control character makes NewRequestWithContext fail.
	_, err := e.downloadPoster(context.Background(), "http://exa\x00mple", t.TempDir())
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

// ---------------------------------------------------------------------------
// seriesDirPath / buildSeriesFromPlaylist
// ---------------------------------------------------------------------------

// runHLS downloads a poster when the playlist carries one, so the cover-art
// path inside runHLS is exercised end-to-end.
func TestRunHLS_DownloadsPoster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("POSTER"))
	}))
	defer srv.Close()

	pl := makePlaylist(1)
	pl.Poster = srv.URL
	hls := newFakeHLS(nil)
	e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: pl})
	// Point output under a temp dir so the poster temp file is writable and the
	// deferred os.Remove cleanup runs against a real path.
	dir := t.TempDir()
	e.deps.OutputLayout = &mockOutputLayout{path: filepath.Join(dir, "ep.mkv")}

	res, err := e.runHLS(context.Background(), retryTestConfig())
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", res.Succeeded)
	}
}

// backoffFor without an override uses the production episodeRetryBackoff.
func TestBackoffFor_ProductionDefault(t *testing.T) {
	e := &engine{} // retryBackoff nil
	if got, want := e.backoffFor(2), episodeRetryBackoff(2); got != want {
		t.Errorf("backoffFor(2) = %s, want production %s", got, want)
	}
	e2 := &engine{retryBackoff: func(int) time.Duration { return 5 * time.Second }}
	if got := e2.backoffFor(2); got != 5*time.Second {
		t.Errorf("override backoffFor(2) = %s, want 5s", got)
	}
}

func TestBuildSeriesFromPlaylist_GroupsAndSortsSeasons(t *testing.T) {
	e := &engine{}
	pl := &domain.PagePlaylist{
		ItemID: 7,
		Title:  "My Show",
		Poster: "http://p",
		Episodes: []domain.PageEpisode{
			{Season: 2, Episode: 1, ManifestURL: "u21", Duration: 60},
			{Season: 1, Episode: 2, ManifestURL: "u12"},
			{Season: 1, Episode: 1, ManifestURL: "u11"},
		},
	}
	series := e.buildSeriesFromPlaylist(pl, retryTestConfig())
	if series.ID != "7" {
		t.Errorf("ID = %q, want 7", series.ID)
	}
	if len(series.Seasons) != 2 {
		t.Fatalf("seasons = %d, want 2", len(series.Seasons))
	}
	// Seasons sorted ascending.
	if series.Seasons[0].Number != 1 || series.Seasons[1].Number != 2 {
		t.Errorf("seasons not sorted: %d, %d", series.Seasons[0].Number, series.Seasons[1].Number)
	}
	// Episodes within season 1 sorted ascending.
	s1 := series.Seasons[0].Episodes
	if len(s1) != 2 || s1[0].Key.Episode != 1 || s1[1].Key.Episode != 2 {
		t.Errorf("season 1 episodes not sorted: %+v", s1)
	}
	// Duration converted to seconds.
	if series.Seasons[1].Episodes[0].Duration != 60*time.Second {
		t.Errorf("duration = %v, want 60s", series.Seasons[1].Episodes[0].Duration)
	}
}
