package kinopub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// Simulate verified on-disk fragments surviving across engine instances.
type retainedCacheHLS struct {
	*fakeHLSDownloader
	err               error
	calls, downloaded int
}

func (h *retainedCacheHLS) DownloadEpisode(_ context.Context, _ string, _ domain.Quality, path string, _ domain.EpisodeKey, _ domain.ProgressSink) (*domain.HLSDownloadResult, error) {
	h.calls++
	dir := path + ".hls-tmp"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	fragment := filepath.Join(dir, "verified.ts")
	if _, err := os.Stat(fragment); os.IsNotExist(err) {
		h.downloaded++
		if err := os.WriteFile(fragment, []byte("complete fragment"), 0600); err != nil {
			return nil, err
		}
	}
	if h.err != nil {
		return nil, h.err
	}
	return &domain.HLSDownloadResult{VideoPath: fragment, TempDir: dir, Resolution: "1280x720"}, nil
}

type retainedCacheMuxer struct {
	mockDownloader
	err     error
	started chan struct{}
}

func (m *retainedCacheMuxer) MuxHLS(ctx context.Context, job domain.Job, hls *domain.HLSDownloadResult) error {
	if m.started != nil {
		close(m.started)
		<-ctx.Done()
		return ctx.Err()
	}
	if m.err != nil {
		return m.err
	}
	data, err := os.ReadFile(hls.VideoPath)
	if err != nil {
		return err
	}
	return os.WriteFile(job.OutPath, data, 0600)
}

func TestFailedDownloadAndMuxKeepVerifiedFragmentsForRestart(t *testing.T) {
	for _, kind := range []string{"retry budget", "authorization", "mux"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "episode.mkv")
			h := &retainedCacheHLS{fakeHLSDownloader: newFakeHLS(nil)}
			mux := &retainedCacheMuxer{}
			attempts := 1
			switch kind {
			case "retry budget":
				h.err = errors.New("unexpected EOF")
				attempts = maxEpisodeAttempts
			case "authorization":
				h.err = errors.New("HTTP 401")
			case "mux":
				mux.err = errors.New("ffmpeg failed")
			}
			makeEngine := func() *engine {
				e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
				e.deps.OutputLayout = &mockOutputLayout{path: path}
				e.deps.Downloader = mux
				return e
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			res, err := makeEngine().runHLS(ctx, retryTestConfig())
			if err != nil || res.Failed != 1 || h.calls != attempts {
				t.Fatalf("failure not accounted for: %+v %v calls=%d", res, err, h.calls)
			}
			cache := path + ".ts.hls-tmp"
			data, err := os.ReadFile(filepath.Join(cache, "verified.ts"))
			if err != nil || string(data) != "complete fragment" {
				t.Fatalf("verified data lost after %s: %v", kind, err)
			}
			// A fresh engine models Retry after restarting the application.
			h.err = nil
			mux.err = nil
			res, err = makeEngine().runHLS(ctx, retryTestConfig())
			if err != nil || res.Succeeded != 1 || h.downloaded != 1 {
				t.Fatalf("restart redownloaded data: %+v %v downloads=%d", res, err, h.downloaded)
			}
			if _, err = os.Stat(cache); !os.IsNotExist(err) {
				t.Fatal("successful mux left cache", err)
			}
			if data, err = os.ReadFile(path); err != nil || string(data) != "complete fragment" {
				t.Fatal("final file missing", err)
			}
		})
	}
}

func TestPauseDuringMuxPreservesCacheAndCancelRemovesIt(t *testing.T) {
	for _, paused := range []bool{true, false} {
		name := "cancel"
		if paused {
			name = "pause"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "episode.mkv")
			h := &retainedCacheHLS{fakeHLSDownloader: newFakeHLS(nil)}
			mux := &retainedCacheMuxer{started: make(chan struct{})}
			e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
			e.deps.OutputLayout = &mockOutputLayout{path: path}
			e.deps.Downloader = mux
			e.deps.Paused = func() bool { return paused }
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan domain.RunResult, 1)
			go func() { res, _ := e.runHLS(ctx, retryTestConfig()); done <- res }()
			select {
			case <-mux.started:
			case <-ctx.Done():
				t.Fatal("mux never started")
			}
			cancel()
			var res domain.RunResult
			select {
			case res = <-done:
			case <-time.After(time.Second):
				t.Fatal("mux cancellation stuck")
			}
			_, err := os.Stat(filepath.Join(path+".ts.hls-tmp", "verified.ts"))
			if paused {
				if err != nil || res.Failed != 0 {
					t.Fatalf("pause lost data or counted failure: %v %+v", err, res)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("explicit cancel retained unwanted cache", err)
			}
		})
	}
}
