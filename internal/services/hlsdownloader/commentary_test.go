package hlsdownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestOriginalDownloadNeverFetchesCommentary(t *testing.T) {
	for _, audioOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("audioOnly=%t", audioOnly), func(t *testing.T) {
			var commentaryHits atomic.Int32
			master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"18. Оригинал (ENG)\",LANGUAGE=\"eng\",URI=\"/original.m3u8\"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"19. Оригинал. Комментарии (ENG)\",LANGUAGE=\"eng\",URI=\"/comments.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,AUDIO=\"a\"\n/video.m3u8\n"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "comments") {
					commentaryHits.Add(1)
				}
				switch r.URL.Path {
				case "/master.m3u8":
					fmt.Fprint(w, master)
				case "/video.m3u8":
					fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\n/video.ts\n#EXT-X-ENDLIST\n")
				case "/original.m3u8":
					fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\n/original.ts\n#EXT-X-ENDLIST\n")
				case "/comments.m3u8":
					fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\n/comments.ts\n#EXT-X-ENDLIST\n")
				case "/video.ts":
					fmt.Fprint(w, "video")
				case "/original.ts":
					fmt.Fprint(w, "original soundtrack")
				case "/comments.ts":
					fmt.Fprint(w, "commentary")
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			d := newTestDownloader(t, srv.Client())
			// The same selection persisted by the user's failed run, before new excludes.
			d.SetAudioPreference(domain.AudioPreference{Specs: []domain.AudioSpec{{Require: []string{"Оригинал"}}}})
			out := filepath.Join(t.TempDir(), "episode.ts")
			var result *domain.HLSDownloadResult
			var err error
			if audioOnly {
				result, err = d.DownloadAudio(context.Background(), srv.URL+"/master.m3u8", "720p", out, domain.EpisodeKey{}, nil, []string{"Russian dub"})
			} else {
				result, err = d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", out, domain.EpisodeKey{}, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if commentaryHits.Load() != 0 {
				t.Fatalf("made %d commentary requests", commentaryHits.Load())
			}
			if len(result.AudioTracks) != 1 {
				t.Fatalf("tracks=%+v", result.AudioTracks)
			}
			data, err := os.ReadFile(result.AudioTracks[0].Path)
			if err != nil || string(data) != "original soundtrack" {
				t.Fatalf("wrong audio=%q %v", data, err)
			}
		})
	}
}
