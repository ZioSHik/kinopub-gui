package kinopub

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// --- Minimal mock implementations for testing ---

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...domain.Field)  {}
func (m *mockLogger) Info(msg string, fields ...domain.Field)   {}
func (m *mockLogger) Warn(msg string, fields ...domain.Field)   {}
func (m *mockLogger) Error(msg string, fields ...domain.Field)  {}
func (m *mockLogger) With(fields ...domain.Field) domain.Logger { return m }
func (m *mockLogger) Component(name string) domain.Logger       { return m }

type mockInputResolver struct {
	source domain.FeedSource
	err    error
}

func (m *mockInputResolver) Classify(rawURL string) (domain.InputClass, error) {
	return domain.ClassPodcastFeed, nil
}
func (m *mockInputResolver) Resolve(ctx context.Context, rawURL string) (domain.FeedSource, error) {
	return m.source, m.err
}

type mockFeedParser struct {
	series domain.Series
	err    error
}

func (m *mockFeedParser) Parse(ctx context.Context, src domain.FeedSource) (domain.Series, error) {
	return m.series, m.err
}

type mockMediaResolver struct {
	media domain.ResolvedMedia
	err   error
}

func (m *mockMediaResolver) Resolve(ctx context.Context, ep domain.Episode, pref domain.QualityPref) (domain.ResolvedMedia, error) {
	return m.media, m.err
}

type mockScheduler struct {
	summary domain.RunSummary
}

func (m *mockScheduler) Run(ctx context.Context, jobs []domain.Job, exec domain.JobExecutor) domain.RunSummary {
	return m.summary
}

type mockDownloader struct{}

func (m *mockDownloader) Download(ctx context.Context, job domain.Job, sink domain.ProgressSink) error {
	return nil
}

type mockProxyProvider struct{}

func (m *mockProxyProvider) HTTPClient() *http.Client { return http.DefaultClient }
func (m *mockProxyProvider) FFmpegEnv() ([]string, error) {
	return nil, nil
}
func (m *mockProxyProvider) Mode() domain.ProxyMode { return domain.ProxyDirect }

type mockProgressReporter struct {
	mu        sync.Mutex // guards the slices against concurrent worker callbacks
	started   bool
	stopped   bool
	completed []domain.EpisodeKey
	failed    []domain.EpisodeKey
}

func (m *mockProgressReporter) Start(plan domain.SeriesPlan)         { m.started = true }
func (m *mockProgressReporter) EpisodeStarted(key domain.EpisodeKey) {}
func (m *mockProgressReporter) TrackProgress(key domain.EpisodeKey, track domain.TrackRef, percent int) {
}
func (m *mockProgressReporter) EpisodeCompleted(key domain.EpisodeKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completed = append(m.completed, key)
}
func (m *mockProgressReporter) EpisodeFailed(key domain.EpisodeKey, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failed = append(m.failed, key)
}
func (m *mockProgressReporter) Stop() { m.stopped = true }

type mockStateStore struct {
	mu        sync.Mutex // guards completed against concurrent worker writes
	state     domain.DownloadState
	completed map[domain.EpisodeKey]bool
}

func (m *mockStateStore) Load(ctx context.Context, series domain.SeriesID) (domain.DownloadState, error) {
	return m.state, nil
}
func (m *mockStateStore) MarkCompleted(ctx context.Context, info domain.CompletedInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.completed == nil {
		m.completed = make(map[domain.EpisodeKey]bool)
	}
	m.completed[info.Key] = true
	return nil
}
func (m *mockStateStore) SetMetadata(ctx context.Context, series domain.SeriesID, meta domain.SeriesMetadata) error {
	return nil
}
func (m *mockStateStore) IsCompleted(state domain.DownloadState, key domain.EpisodeKey) bool {
	return false
}

type mockOutputLayout struct {
	path string
	err  error
}

func (m *mockOutputLayout) EpisodePath(root string, series domain.Series, ep domain.Episode) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.path != "" {
		return m.path, nil
	}
	return "/tmp/out/S01E01.mkv", nil
}
func (m *mockOutputLayout) EnsureDirs(path string) error { return nil }

// --- Tests ---

func TestNew_AllDepsProvided(t *testing.T) {
	deps := validDeps()
	app, err := New(deps)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}
}

func TestNew_NilLogger(t *testing.T) {
	deps := validDeps()
	deps.Logger = nil
	_, err := New(deps)
	if err == nil {
		t.Fatal("expected error for nil Logger")
	}
	if !errors.Is(err, domain.ErrMissingDependency) {
		t.Fatalf("expected ErrMissingDependency, got: %v", err)
	}
}

func TestNew_NilInputResolver(t *testing.T) {
	deps := validDeps()
	deps.InputResolver = nil
	_, err := New(deps)
	if err == nil {
		t.Fatal("expected error for nil InputResolver")
	}
	if !errors.Is(err, domain.ErrMissingDependency) {
		t.Fatalf("expected ErrMissingDependency, got: %v", err)
	}
}

func TestNew_NilScheduler(t *testing.T) {
	deps := validDeps()
	deps.Scheduler = nil
	_, err := New(deps)
	if err == nil {
		t.Fatal("expected error for nil Scheduler")
	}
	if !errors.Is(err, domain.ErrMissingDependency) {
		t.Fatalf("expected ErrMissingDependency, got: %v", err)
	}
}

func TestRun_DryRun(t *testing.T) {
	deps := validDeps()
	deps.InputResolver = &mockInputResolver{source: domain.FeedSource{ID: "123", Token: "abc"}}
	deps.FeedParser = &mockFeedParser{series: testSeries()}
	deps.MediaResolver = &mockMediaResolver{media: domain.ResolvedMedia{
		Video: domain.VideoTrack{Index: 0, Resolution: "1920x1080"},
	}}

	app, err := New(deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := domain.RunConfig{
		InputURL:   "https://kino.pub/podcast/get/123/abc",
		DryRun:     true,
		SeasonSel:  domain.Selection{All: true},
		EpisodeSel: domain.Selection{All: true},
	}

	result, err := app.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected Total=2, got %d", result.Total)
	}
}

func TestRun_FullDownload(t *testing.T) {
	reporter := &mockProgressReporter{}
	stateStore := &mockStateStore{state: domain.DownloadState{}}

	deps := validDeps()
	deps.InputResolver = &mockInputResolver{source: domain.FeedSource{ID: "123", Token: "abc"}}
	deps.FeedParser = &mockFeedParser{series: testSeries()}
	deps.MediaResolver = &mockMediaResolver{media: domain.ResolvedMedia{
		Video: domain.VideoTrack{Index: 0, Resolution: "1920x1080"},
	}}
	deps.Scheduler = &mockScheduler{summary: domain.RunSummary{
		Total:     2,
		Succeeded: 2,
		Failed:    0,
		Outcomes: []domain.JobOutcome{
			{Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}, Succeeded: true},
			{Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 2}, Succeeded: true},
		},
	}}
	deps.ProgressReporter = reporter
	deps.StateStore = stateStore

	app, err := New(deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := domain.RunConfig{
		InputURL:   "https://kino.pub/podcast/get/123/abc",
		SeasonSel:  domain.Selection{All: true},
		EpisodeSel: domain.Selection{All: true},
	}

	result, err := app.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Succeeded != 2 {
		t.Errorf("expected Succeeded=2, got %d", result.Succeeded)
	}
	if !reporter.started {
		t.Error("expected progress reporter to be started")
	}
	if !reporter.stopped {
		t.Error("expected progress reporter to be stopped")
	}
	if len(reporter.completed) != 2 {
		t.Errorf("expected 2 completed episodes, got %d", len(reporter.completed))
	}
	if !stateStore.completed[domain.EpisodeKey{Series: "test", Season: 1, Episode: 1}] {
		t.Error("expected episode S01E01 to be marked completed in state store")
	}
}

func TestRun_ResolveError(t *testing.T) {
	deps := validDeps()
	deps.InputResolver = &mockInputResolver{err: domain.ErrInvalidInputURL}

	app, err := New(deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := domain.RunConfig{
		InputURL:   "https://example.com/bad",
		SeasonSel:  domain.Selection{All: true},
		EpisodeSel: domain.Selection{All: true},
	}

	_, err = app.Run(context.Background(), cfg)
	if !errors.Is(err, domain.ErrInvalidInputURL) {
		t.Fatalf("expected ErrInvalidInputURL, got: %v", err)
	}
}

// --- Helpers ---

func validDeps() Dependencies {
	return Dependencies{
		Logger:           &mockLogger{},
		InputResolver:    &mockInputResolver{},
		FeedParser:       &mockFeedParser{},
		MediaResolver:    &mockMediaResolver{},
		Scheduler:        &mockScheduler{},
		Downloader:       &mockDownloader{},
		ProxyProvider:    &mockProxyProvider{},
		ProgressReporter: &mockProgressReporter{},
		StateStore:       &mockStateStore{state: domain.DownloadState{}},
		OutputLayout:     &mockOutputLayout{},
	}
}

func testSeries() domain.Series {
	return domain.Series{
		ID:    "test",
		Title: "Test Series",
		Seasons: []domain.Season{
			{
				Number: 1,
				Episodes: []domain.Episode{
					{
						Key:   domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
						Title: "Episode 1",
						MediaSources: []domain.MediaSource{
							{Kind: domain.MediaHLS, URL: "https://example.com/s1e1.m3u8", Quality: "1080p"},
						},
					},
					{
						Key:   domain.EpisodeKey{Series: "test", Season: 1, Episode: 2},
						Title: "Episode 2",
						MediaSources: []domain.MediaSource{
							{Kind: domain.MediaHLS, URL: "https://example.com/s1e2.m3u8", Quality: "1080p"},
						},
					},
				},
			},
		},
	}
}
