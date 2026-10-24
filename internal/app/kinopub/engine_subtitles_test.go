package kinopub

import (
	"context"
	"errors"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

type subtitleHLS struct {
	*fakeHLSDownloader
	sources []domain.SubtitleSource
	err     error
}

func (f *subtitleHLS) DownloadSubtitles(_ context.Context, sources []domain.SubtitleSource, _ string) ([]domain.HLSSubtitleTrack, error) {
	f.sources = sources
	return []domain.HLSSubtitleTrack{{Path: "/sub.srt", Language: "eng"}}, f.err
}

type subtitleMuxer struct {
	mockDownloader
	tracks []domain.HLSSubtitleTrack
}

func (m *subtitleMuxer) MuxHLS(_ context.Context, _ domain.Job, hls *domain.HLSDownloadResult) error {
	m.tracks = hls.SubtitleTracks
	return nil
}

func TestRunHLSCarriesAPISubtitlesToMuxer(t *testing.T) {
	for _, fail := range []bool{false, true} {
		pl := makePlaylist(1)
		pl.Episodes[0].Subtitles = []domain.SubtitleSource{{URL: "https://cdn.test/sub.srt", Language: "eng"}}
		hls := &subtitleHLS{fakeHLSDownloader: newFakeHLS(nil)}
		if fail {
			hls.err = errors.New("subtitle unavailable")
		}
		e, _, _ := newRetryTestEngine(hls, &fakePageScraper{playlist: pl})
		mux := &subtitleMuxer{}
		e.deps.Downloader = mux
		result, _ := e.runHLS(context.Background(), retryTestConfig())
		if len(hls.sources) != 1 || hls.sources[0].Language != "eng" {
			t.Fatal("API subtitle metadata did not reach downloader")
		}
		if fail {
			if result.Succeeded != 0 || result.Failed != 1 || len(mux.tracks) != 0 {
				t.Fatalf("subtitle failure must not produce an incomplete success: %+v", result)
			}
		} else if result.Succeeded != 1 || len(mux.tracks) != 1 || mux.tracks[0].Language != "eng" {
			t.Fatalf("subtitles did not reach muxer: %+v, %+v", result, mux.tracks)
		}
	}
}
