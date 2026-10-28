package kinopub

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/outputlayout"
	"github.com/ZioSHik/kinopub-gui/internal/services/statestore"
)

type variantHLS struct {
	*fakeHLSDownloader
	pref domain.AudioPreference
}

func (h *variantHLS) SetAudioPreference(p domain.AudioPreference) { h.pref = p }
func (h *variantHLS) DownloadEpisode(ctx context.Context, url string, q domain.Quality, out string, key domain.EpisodeKey, sink domain.ProgressSink) (*domain.HLSDownloadResult, error) {
	r, err := h.fakeHLSDownloader.DownloadEpisode(ctx, url, q, out, key, sink)
	if err != nil {
		return nil, err
	}
	if q == "1080p" {
		r.Resolution = "1920x1080"
	}
	if len(h.pref.Include) > 0 {
		r.AudioTracks = []domain.HLSAudioTrack{{Name: h.pref.Include[0], Language: "rus"}}
	}
	return r, nil
}

func (h *variantHLS) DownloadAudio(ctx context.Context, url string, q domain.Quality, out string, key domain.EpisodeKey, sink domain.ProgressSink, existing []string) (*domain.HLSDownloadResult, error) {
	result, err := h.DownloadEpisode(ctx, url, q, out, key, sink)
	if err != nil {
		return nil, err
	}
	result.VideoPath = "" // engine must substitute the completed file
	return result, nil
}

type variantMux struct{ mockDownloader }

func (*variantMux) MuxHLS(_ context.Context, job domain.Job, r *domain.HLSDownloadResult) error {
	var names []string
	for _, a := range r.AudioTracks {
		names = append(names, a.Name)
	}
	if r.PreserveExisting {
		data, err := os.ReadFile(r.VideoPath)
		if err != nil {
			return err
		}
		return os.WriteFile(job.OutPath, append(data, []byte(","+strings.Join(names, ","))...), 0600)
	}
	return os.WriteFile(job.OutPath, []byte(r.Resolution+"|"+strings.Join(names, ",")), 0600)
}

func TestExistingEpisodeAcceptsChangedSelection(t *testing.T) {
	for _, kind := range []string{"audio", "resolution"} {
		t.Run(kind, func(t *testing.T) {
			h := &variantHLS{fakeHLSDownloader: newFakeHLS(nil)}
			pl := makePlaylist(1)
			e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: pl})
			cfg := retryTestConfig()
			cfg.OutputPath = t.TempDir()
			cfg.AudioPref.Include = []string{"Studio A"}
			e.deps.OutputLayout = outputlayout.New(domain.ContainerMKV)
			store := statestore.New(cfg.OutputPath, &mockLogger{})
			e.deps.StateStore = store
			e.deps.Downloader = &variantMux{}
			if r, err := e.runHLS(context.Background(), cfg); err != nil || r.Succeeded != 1 {
				t.Fatalf("initial=%+v err=%v", r, err)
			}
			series := e.buildSeriesFromPlaylist(pl, cfg)
			state, _ := store.Load(context.Background(), series.ID)
			oldPath := state.Completed["S1E1"].Path
			oldBytes, _ := os.ReadFile(oldPath)
			if kind == "audio" {
				cfg.AudioPref.Include = []string{"Studio B"}
			} else {
				cfg.Quality = "1080p"
			}
			r, err := e.runHLS(context.Background(), cfg)
			if err != nil || r.Succeeded != 1 {
				t.Fatalf("requested %s change was ignored: result=%+v err=%v", kind, r, err)
			}
			state, _ = store.Load(context.Background(), series.ID)
			rec := state.Completed["S1E1"]
			if kind == "resolution" {
				data, err := os.ReadFile(oldPath)
				if err != nil || !bytes.Equal(data, oldBytes) {
					t.Fatalf("old quality overwritten: %q %v", data, err)
				}
				if len(rec.Files()) != 2 {
					t.Fatalf("versions=%+v", rec.Files())
				}
			} else if len(rec.Audio) != 2 || rec.Audio[0] != "Studio A" || rec.Audio[1] != "Studio B" {
				t.Fatalf("audio was replaced rather than added: %v", rec.Audio)
			}
			r, err = e.runHLS(context.Background(), cfg)
			if err != nil || r.Total != 0 {
				t.Fatalf("same selection should be skipped: %+v %v", r, err)
			}
			cfg.Quality = "720p"
			cfg.AudioPref.Include = []string{"Studio A"}
			r, err = e.runHLS(context.Background(), cfg)
			if err != nil || r.Total != 0 {
				t.Fatalf("earlier selection was lost: %+v %v", r, err)
			}

		})
	}
}

type failingStore struct{ *mockStateStore }

func (*failingStore) MarkCompleted(context.Context, domain.CompletedInfo) error {
	return errors.New("state persistence failed")
}
func TestPersistenceFailureIsNotSuccess(t *testing.T) {
	e, _, _ := newRetryTestEngine(newFakeHLS(nil), &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.StateStore = &failingStore{mockStateStore: &mockStateStore{}}
	r, err := e.runHLS(context.Background(), retryTestConfig())
	if err == nil && r.Succeeded > 0 {
		t.Fatalf("state write failed but download reports success: %+v", r)
	}
}
