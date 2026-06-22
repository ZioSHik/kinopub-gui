package kinopub

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// --- Minimal mock implementations for testing ---

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...domain.Field)  {}
func (m *mockLogger) Info(msg string, fields ...domain.Field)   {}
func (m *mockLogger) Warn(msg string, fields ...domain.Field)   {}
func (m *mockLogger) Error(msg string, fields ...domain.Field)  {}
func (m *mockLogger) With(fields ...domain.Field) domain.Logger { return m }
func (m *mockLogger) Component(name string) domain.Logger       { return m }

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

// --- Helpers ---

func validDeps() Dependencies {
	return Dependencies{
		Logger:           &mockLogger{},
		Scheduler:        &mockScheduler{},
		Downloader:       &mockDownloader{},
		ProxyProvider:    &mockProxyProvider{},
		ProgressReporter: &mockProgressReporter{},
		StateStore:       &mockStateStore{state: domain.DownloadState{}},
		OutputLayout:     &mockOutputLayout{},
	}
}
