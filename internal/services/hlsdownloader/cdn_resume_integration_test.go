package hlsdownloader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

type cdnFixtureTransport func(*http.Request) (*http.Response, error)

func (f cdnFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCDNSessionFailureResumesVerifiedSegmentsAfterRestartAndTokenRefresh(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	var cached string
	firstSession := true
	client := &http.Client{Transport: cdnFixtureTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		name := filepath.Base(r.URL.Path)
		body := ""
		status := http.StatusOK
		switch name {
		case "master.m3u8":
			body = "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=1280x720\nvideo.m3u8\n"
		case "video.m3u8":
			body = "#EXTM3U\n#EXTINF:1,\n1.ts\n#EXTINF:1,\n2.ts\n#EXT-X-ENDLIST\n"
		default:
			hits[name]++
			if firstSession && cached != "" {
				status = 403
				body = "user session limit reached"
			} else {
				body = name
				cached = name
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	lim := NewCDNLimiter(nopLogger{})
	lim.limit = 1 // deterministically finish one fragment before the CDN rejects another
	out := filepath.Join(t.TempDir(), "episode.ts")
	newDownloader := func(lim *Limiter) *Downloader { return newTestDownloader(t, client, WithLimiter(lim)) }
	base := "https://media.ams-static-01.cdntogo.net/hls/"
	_, err := newDownloader(lim).DownloadEpisode(context.Background(), base+"old-token/movie/master.m3u8", "max", out, domain.EpisodeKey{}, nil)
	if !errors.Is(err, domain.ErrCDNSessionLimit) {
		t.Fatal("expected CDN session failure", err)
	}
	mu.Lock()
	firstSession = false
	preserved := cached
	mu.Unlock()
	// Fresh controller and downloader model a process restart. Only the signed
	// session prefix changes; the verified fragment must not be requested again.
	result, err := newDownloader(NewCDNLimiter(nopLogger{})).DownloadEpisode(context.Background(), base+"new-token/movie/master.m3u8", "max", out, domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.VideoPath)
	if err != nil || string(data) != "1.ts2.ts" {
		t.Fatalf("resumed output corrupted: %q %v", data, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits[preserved] != 1 {
		t.Fatalf("verified fragment %q was fetched again: %v", preserved, hits)
	}
}
