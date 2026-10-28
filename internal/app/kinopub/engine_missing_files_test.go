package kinopub

import (
	"context"
	"os"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/outputlayout"
	"github.com/ZioSHik/kinopub-gui/internal/services/statestore"
)

type fileWritingMuxer struct{ mockDownloader }

func (*fileWritingMuxer) MuxHLS(_ context.Context, job domain.Job, _ *domain.HLSDownloadResult) error {
	return os.WriteFile(job.OutPath, []byte("downloaded video"), 0600)
}

func TestRunHLSRedownloadsDeletedFiles(t *testing.T) {
	for _, tc := range []struct {
		name         string
		episodes     int
		relativeRoot bool
	}{{"movie", 1, false}, {"series", 3, false}, {"relative output root", 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			playlist := makePlaylist(tc.episodes)
			hls := newFakeHLS(nil)
			e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: playlist})
			cfg := retryTestConfig()
			if tc.relativeRoot {
				// Keep the relative root on the working directory's volume;
				// Windows runners can put t.TempDir on a different drive.
				root, err := os.MkdirTemp(".", ".redownload-test-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(root) })
				cfg.OutputPath = root
			} else {
				cfg.OutputPath = t.TempDir()
			}
			cfg.MaxConcurrency = 1
			layout := outputlayout.New(domain.ContainerMKV)
			e.deps.OutputLayout = layout
			e.deps.StateStore = statestore.New(cfg.OutputPath, &mockLogger{})
			e.deps.Downloader = &fileWritingMuxer{}
			result, err := e.runHLS(ctx, cfg)
			if err != nil || result.Succeeded != tc.episodes {
				t.Fatalf("initial download: result=%+v err=%v", result, err)
			}
			series := e.buildSeriesFromPlaylist(playlist, cfg)
			first := series.Seasons[0].Episodes[0]
			path, err := layout.EpisodePath(cfg.OutputPath, series, first)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			result, err = e.runHLS(ctx, cfg)
			if err != nil || result.Total != 1 || result.Succeeded != 1 {
				t.Fatalf("re-download must restore only the missing file: result=%+v err=%v", result, err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "downloaded video" {
				t.Fatalf("file was not restored: data=%q err=%v", data, err)
			}
			for i := 1; i <= tc.episodes; i++ {
				key := domain.EpisodeKey{Series: "42", Season: 1, Episode: i}
				want := 1
				if i == 1 {
					want = 2
				}
				if got := hls.calls[key]; got != want {
					t.Errorf("episode %d downloaded %d times, want %d", i, got, want)
				}
			}
			result, err = e.runHLS(ctx, cfg)
			if err != nil || result.Total != 0 {
				t.Fatalf("existing files must still be skipped: result=%+v err=%v", result, err)
			}
		})
	}
}
