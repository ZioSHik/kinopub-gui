package hlsdownloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestCeilingIncludesInitSegments(t *testing.T) {
	var active, peak atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Russian\",LANGUAGE=\"rus\",URI=\"/audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=1280x720,AUDIO=\"a\"\n/video.m3u8\n"
	video := "#EXTM3U\n#EXTINF:1,\n/v1.ts\n#EXTINF:1,\n/v2.ts\n#EXT-X-ENDLIST\n"
	audio := "#EXTM3U\n#EXT-X-MAP:URI=\"/init.mp4\"\n#EXTINF:1,\n/a1.m4s\n#EXT-X-ENDLIST\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(w, master)
			return
		case "/video.m3u8":
			fmt.Fprint(w, video)
			return
		case "/audio.m3u8":
			fmt.Fprint(w, audio)
			return
		}
		now := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
		}
		fmt.Fprint(w, "fragment")
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "episode.ts")
	ap, err := parseMediaPlaylist(strings.NewReader(audio), srv.URL+"/audio.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(out+".hls-tmp", "audio_0")
	if err := prepareTrackCache(cache, ap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "seg_00000.ts"), []byte("cached"), 0600); err != nil {
		t.Fatal(err)
	}
	d := newTestDownloader(t, srv.Client(), WithLimiter(NewLimiterWithCeiling(2, 2, nopLogger{})))
	_, err = d.DownloadEpisode(ctx, srv.URL+"/master.m3u8", "max", out, domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := peak.Load(); got > 2 {
		t.Fatalf("configured CDN request ceiling=2, observed %d simultaneous fragment requests", got)
	}
}

func TestCeilingIncludesSubtitleSegments(t *testing.T) {
	var active, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\n/sub.vtt\n#EXT-X-ENDLIST\n")
			return
		}
		now := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); now > old; old = peak.Load() {
			if peak.CompareAndSwap(old, now) {
				break
			}
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-r.Context().Done():
		}
		fmt.Fprint(w, "WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nHello\n")
	}))
	defer srv.Close()
	master := &MasterPlaylist{}
	for i := 0; i < 4; i++ {
		master.Subtitles = append(master.Subtitles, SubtitleRendition{GroupID: "s", Name: fmt.Sprint(i), URI: fmt.Sprintf("%s/sub%d.m3u8", srv.URL, i)})
	}
	d := newTestDownloader(t, srv.Client(), WithLimiter(NewLimiterWithCeiling(2, 2, nopLogger{})))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tracks, err := d.downloadSubtitleRenditions(ctx, master, Variant{SubtitleGroup: "s"}, t.TempDir())
	if err != nil || len(tracks) != 4 {
		t.Fatalf("lost subtitle tracks: tracks=%d err=%v", len(tracks), err)
	}
	if got := peak.Load(); got > 2 {
		t.Fatalf("configured CDN request ceiling=2, observed %d subtitle requests", got)
	}
}

func TestSubtitleSessionLimitPausesRequestsWithoutDroppingTracks(t *testing.T) {
	for _, stage := range []string{"external", "hls playlist", "hls segment"} {
		t.Run(stage, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if stage == "hls segment" && strings.HasSuffix(r.URL.Path, ".m3u8") {
					fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\n/sub.vtt\n#EXT-X-ENDLIST\n")
					return
				}
				http.Error(w, "user session limit reached", http.StatusForbidden)
			}))
			defer srv.Close()
			lim := NewLimiterWithCeiling(2, 2, nopLogger{})
			d := newTestDownloader(t, srv.Client(), WithLimiter(lim))
			var err error
			wantHits := int32(1)
			if stage == "external" {
				_, err = d.DownloadSubtitles(context.Background(), []domain.SubtitleSource{{URL: srv.URL + "/sub.vtt"}}, t.TempDir())
			} else {
				master := &MasterPlaylist{Subtitles: []SubtitleRendition{{GroupID: "s", Name: "English", URI: srv.URL + "/sub.m3u8"}}}
				_, err = d.downloadSubtitleRenditions(context.Background(), master, Variant{SubtitleGroup: "s"}, t.TempDir())
				if stage == "hls segment" {
					wantHits++
				}
			}
			if !errors.Is(err, domain.ErrCDNSessionLimit) || hits.Load() != wantHits {
				t.Fatalf("session limit dropped or retried: hits=%d err=%v", hits.Load(), err)
			}
			if !lim.Stats().Throttled {
				t.Fatal("subtitle response did not activate shared cooldown")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err = d.downloadSubtitle(ctx, srv.URL+"/sub.vtt", filepath.Join(t.TempDir(), "sub.vtt"))
			if !errors.Is(err, context.DeadlineExceeded) || hits.Load() != wantHits {
				t.Fatalf("subtitle request bypassed cooldown: hits=%d err=%v", hits.Load(), err)
			}
		})
	}
}
