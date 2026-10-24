package hlsdownloader

import (
	"context"
	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadOriginalUsesGenericEnglishTrack(t *testing.T) {
	for _, tc := range []struct {
		name              string
		available, broken bool
	}{{"available", true, false}, {"missing", false, false}, {"broken playlist", true, true}} {
		available := tc.available
		t.Run(tc.name, func(t *testing.T) {
			master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Многоголосый (RUS)\",LANGUAGE=\"rus\",URI=\"/ru.m3u8\"\n"
			if available {
				master += "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Track 8 (ENG)\",LANGUAGE=\"eng\",URI=\"/en.m3u8\"\n"
			}
			master += "#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,AUDIO=\"a\"\n/video.m3u8\n"
			englishPlaylist := "#EXTM3U\n#EXTINF:10,\n/en.ts\n#EXT-X-ENDLIST\n"
			if tc.broken {
				englishPlaylist = "#EXTM3U\n#EXTINF:invalid,\n/en.ts\n"
			}
			srv := hlsTestServer(t, map[string]string{
				"/master.m3u8": master,
				"/video.m3u8":  "#EXTM3U\n#EXTINF:10,\n/video.ts\n#EXT-X-ENDLIST\n", "/video.ts": "video",
				"/ru.m3u8": "#EXTM3U\n#EXTINF:10,\n/ru.ts\n#EXT-X-ENDLIST\n", "/ru.ts": "russian",
				"/en.m3u8": englishPlaylist, "/en.ts": "english",
			})
			defer srv.Close()
			d := newTestDownloader(t, srv.Client())
			d.SetAudioPreference(domain.AudioPreference{OriginalLanguage: "eng", Specs: []domain.AudioSpec{{Require: []string{"Оригинал"}}}})
			result, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", filepath.Join(t.TempDir(), "video.ts"), domain.EpisodeKey{}, nil)
			if tc.broken {
				if err == nil || !strings.Contains(err.Error(), "original audio playlist") {
					t.Fatalf("ignored original playlist error: %v", err)
				}
				return
			}
			if !available {
				if err == nil || !strings.Contains(err.Error(), "refusing to substitute") {
					t.Fatalf("silently substituted Russian: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.AudioTracks) != 1 || result.AudioTracks[0].Language != "eng" || result.AudioFallback {
				t.Fatalf("wrong audio: %+v", result)
			}
			data, err := os.ReadFile(result.AudioTracks[0].Path)
			if err != nil || string(data) != "english" {
				t.Fatalf("wrong audio payload: %q %v", data, err)
			}
		})
	}
}
