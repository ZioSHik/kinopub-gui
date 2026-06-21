package kinopub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// fakeHLSDownloader simulates the HLS downloader. For each episode key it can
// be told to fail a number of times (with a given error) before succeeding.
type fakeHLSDownloader struct {
	mu        sync.Mutex
	failsLeft map[domain.EpisodeKey]int // remaining transient failures per episode
	failErr   error
	calls     map[domain.EpisodeKey]int // total DownloadEpisode calls per episode
	order     []domain.EpisodeKey       // episode call order
}

func newFakeHLS(failErr error) *fakeHLSDownloader {
	return &fakeHLSDownloader{
		failsLeft: make(map[domain.EpisodeKey]int),
		failErr:   failErr,
		calls:     make(map[domain.EpisodeKey]int),
	}
}

func (f *fakeHLSDownloader) DownloadEpisode(_ context.Context, _ string, _ domain.Quality, _ string, key domain.EpisodeKey, _ domain.ProgressSink) (*domain.HLSDownloadResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[key]++
	f.order = append(f.order, key)
	if f.failsLeft[key] > 0 {
		f.failsLeft[key]--
		return nil, f.failErr
	}
	return &domain.HLSDownloadResult{
		Resolution:  "1280x720",
		BitrateKbps: 2000,
		Codec:       "h264",
		VideoPath:   "/tmp/video.ts",
	}, nil
}

func (f *fakeHLSDownloader) ListAudioTracks(context.Context, string, domain.Quality) ([]domain.AudioTrackInfo, error) {
	return nil, nil
}
func (f *fakeHLSDownloader) SetAudioPreference(domain.AudioPreference) {}

// muxingDownloader is a mockDownloader that also satisfies domain.HLSMuxer.
type muxingDownloader struct {
	mockDownloader
}

func (m *muxingDownloader) MuxHLS(context.Context, domain.Job, *domain.HLSDownloadResult) error {
	return nil
}

type fakePageScraper struct {
	playlist *domain.PagePlaylist
}

func (f *fakePageScraper) ExtractAllSeasons(context.Context, string) (*domain.PagePlaylist, error) {
	return f.playlist, nil
}

func makePlaylist(episodes int) *domain.PagePlaylist {
	pl := &domain.PagePlaylist{
		ItemID: 42,
		Title:  "Test Series",
		Seasons: []domain.PageSeason{{Season: 1, Count: episodes}},
	}
	for i := 1; i <= episodes; i++ {
		pl.Episodes = append(pl.Episodes, domain.PageEpisode{
			ManifestURL:  "https://cdn.example/manifest.m3u8",
			MediaID:      i,
			EpisodeTitle: "Ep",
			Season:       1,
			Episode:      i,
		})
	}
	return pl
}

func newRetryTestEngine(hls domain.HLSDownloader, scraper domain.PageScraper) (*engine, *recordingReporter, *mockStateStore) {
	rec := &recordingReporter{}
	ss := &mockStateStore{}
	deps := Dependencies{
		Logger:           &mockLogger{},
		InputResolver:    &mockInputResolver{},
		FeedParser:       &mockFeedParser{},
		MediaResolver:    &mockMediaResolver{},
		Scheduler:        &mockScheduler{},
		Downloader:       &muxingDownloader{},
		ProxyProvider:    &mockProxyProvider{},
		ProgressReporter: rec,
		StateStore:       ss,
		OutputLayout:     &mockOutputLayout{path: "/tmp/out/ep.mkv"},
		HLSDownloader:    hls,
		PageScraper:      scraper,
	}
	e := &engine{
		deps:         deps,
		retryBackoff: func(int) time.Duration { return 0 }, // no waiting in tests
	}
	return e, rec, ss
}

// retryTestConfig builds a RunConfig with selection defaults applied so all
// episodes are considered.
func retryTestConfig() domain.RunConfig {
	cfg := domain.RunConfig{InputURL: "https://kino.pub/item/view/42", Quality: "720p"}
	ApplyDefaults(&cfg)
	return cfg
}

// A transiently-failing episode is retried later and eventually succeeds,
// while the other episodes complete normally.
func TestRunHLS_DeferredRetrySucceeds(t *testing.T) {
	hls := newFakeHLS(errors.New("video track: segment 44 failed: after 5 attempts: context deadline exceeded"))
	// Episode 2 fails twice, then succeeds.
	key2 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 2}
	hls.failsLeft[key2] = 2

	e, rec, ss := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(3)})

	cfg := retryTestConfig()
	res, err := e.runHLS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}

	if res.Succeeded != 3 {
		t.Errorf("Succeeded = %d, want 3", res.Succeeded)
	}
	if res.Failed != 0 {
		t.Errorf("Failed = %d, want 0", res.Failed)
	}
	if got := hls.calls[key2]; got != 3 {
		t.Errorf("episode 2 attempted %d times, want 3", got)
	}
	if len(rec.completed) != 3 {
		t.Errorf("completed reported %d times, want 3", len(rec.completed))
	}
	if len(rec.deferred) != 2 {
		t.Errorf("deferred reported %d times, want 2", len(rec.deferred))
	}
	// All three episodes must be marked completed in the state store.
	for i := 1; i <= 3; i++ {
		k := domain.EpisodeKey{Series: "42", Season: 1, Episode: i}
		if !ss.completed[k] {
			t.Errorf("episode %d not marked completed in state", i)
		}
	}
}

// Interleaving: a deferred episode is retried after the next new episode, not
// only at the very end.
func TestRunHLS_DeferredRetryInterleaves(t *testing.T) {
	hls := newFakeHLS(errors.New("unexpected EOF"))
	key1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	hls.failsLeft[key1] = 1 // E01 fails once, then succeeds on its deferred retry

	e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(3)})

	cfg := retryTestConfig()
	if _, err := e.runHLS(context.Background(), cfg); err != nil {
		t.Fatalf("runHLS error: %v", err)
	}

	// Expected order: E01 (fail) → E02 → E01 (retry, ok) → E03.
	want := []domain.EpisodeKey{
		{Series: "42", Season: 1, Episode: 1},
		{Series: "42", Season: 1, Episode: 2},
		{Series: "42", Season: 1, Episode: 1},
		{Series: "42", Season: 1, Episode: 3},
	}
	if len(hls.order) != len(want) {
		t.Fatalf("call order = %v, want %v", hls.order, want)
	}
	for i := range want {
		if hls.order[i] != want[i] {
			t.Fatalf("call order[%d] = %v, want %v (full: %v)", i, hls.order[i], want[i], hls.order)
		}
	}
}

// A permanent (non-transient) error is not retried.
func TestRunHLS_FatalErrorNotRetried(t *testing.T) {
	hls := newFakeHLS(errors.New("no variants found in master playlist"))
	key1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	hls.failsLeft[key1] = 99 // would keep failing if retried

	e, rec, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(1)})

	cfg := retryTestConfig()
	res, err := e.runHLS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("Failed = %d, want 1", res.Failed)
	}
	if got := hls.calls[key1]; got != 1 {
		t.Errorf("fatal episode attempted %d times, want 1 (no retry)", got)
	}
	if len(rec.deferred) != 0 {
		t.Errorf("fatal error should not defer, got %d deferrals", len(rec.deferred))
	}
}

// An episode that always fails transiently eventually gives up after the
// attempt budget instead of looping forever.
func TestRunHLS_GivesUpAfterBudget(t *testing.T) {
	hls := newFakeHLS(errors.New("context deadline exceeded"))
	key1 := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	hls.failsLeft[key1] = 10000 // never succeeds

	e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: makePlaylist(1)})

	cfg := retryTestConfig()
	res, err := e.runHLS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("Failed = %d, want 1", res.Failed)
	}
	if got := hls.calls[key1]; got != maxEpisodeAttempts {
		t.Errorf("episode attempted %d times, want %d (budget)", got, maxEpisodeAttempts)
	}
}
