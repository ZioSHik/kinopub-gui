package hlsdownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestExactAudioFetchesOnlySelectedRendition(t *testing.T) {
	sources := []domain.AudioSource{
		{Index: 1, Language: "rus", Type: "Многоголосый", Author: "Jask", Codec: "aac", Channels: 6},
		{Index: 15, Language: "ukr", Type: "Многоголосый", Codec: "aac", Channels: 6},
		{Index: 16, Language: "ukr", Type: "Многоголосый", Codec: "aac", Channels: 2},
		{Index: 17, Language: "ukr", Type: "Многоголосый", Author: "НЛО-TV", Codec: "aac", Channels: 2},
		{Index: 20, Language: "rus", Type: "Многоголосый", Author: "Jask", Codec: "ac3", Channels: 6},
	}
	for _, audioOnly := range []bool{false, true} {
		for _, selected := range sources {
			t.Run(fmt.Sprintf("onlyAudio=%t/%s", audioOnly, selected.RecordedName()), func(t *testing.T) {
				var master strings.Builder
				master.WriteString("#EXTM3U\n")
				for _, s := range sources {
					label := s
					label.Codec = ""
					label.Channels = 0
					fmt.Fprintf(&master, "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"%d. %s\",LANGUAGE=\"%s\",URI=\"/audio-%d.m3u8\"\n", s.Index, label.RecordedName(), s.Language, s.Index)
				}
				master.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,AUDIO=\"a\"\n/video.m3u8\n")
				var mu sync.Mutex
				hits := map[string]int{}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					hits[r.URL.Path]++
					mu.Unlock()
					switch {
					case r.URL.Path == "/master.m3u8":
						fmt.Fprint(w, master.String())
					case strings.HasSuffix(r.URL.Path, ".m3u8"):
						fmt.Fprintf(w, "#EXTM3U\n#EXTINF:1,\n%s.ts\n#EXT-X-ENDLIST\n", strings.TrimSuffix(r.URL.Path, ".m3u8"))
					default:
						fmt.Fprint(w, "segment")
					}
				}))
				defer srv.Close()
				d := newTestDownloader(t, srv.Client())
				d.SetAudioPreference(domain.AudioPreference{Specs: []domain.AudioSpec{{Require: []string{"Многоголосый"}, Source: &selected}}, EpisodeSources: map[domain.EpisodeKey][]domain.AudioSource{{}: sources}})
				out := filepath.Join(t.TempDir(), "episode.ts")
				var result *domain.HLSDownloadResult
				var err error
				if audioOnly {
					result, err = d.DownloadAudio(context.Background(), srv.URL+"/master.m3u8", "720p", out, domain.EpisodeKey{}, nil, []string{"existing dub"})
				} else {
					result, err = d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", out, domain.EpisodeKey{}, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(result.AudioTracks) != 1 || result.AudioTracks[0].Name != selected.RecordedName() {
					t.Fatalf("unexpected audio: %+v", result.AudioTracks)
				}
				mu.Lock()
				defer mu.Unlock()
				for _, s := range sources {
					path := fmt.Sprintf("/audio-%d.m3u8", s.Index)
					if s.Index != selected.Index && hits[path] > 0 {
						t.Errorf("fetched unselected audio %s", path)
					}
				}
			})
		}
	}
}

func TestExactAudioRefusesUnverifiableMuxedVariant(t *testing.T) {
	srv := hlsTestServer(t, map[string]string{"/master.m3u8": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720\n/video.m3u8\n"})
	d := newTestDownloader(t, srv.Client())
	d.SetAudioPreference(domain.AudioPreference{Specs: []domain.AudioSpec{{Source: &domain.AudioSource{Language: "eng", Type: "Оригинал"}}}})
	_, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", filepath.Join(t.TempDir(), "episode.ts"), domain.EpisodeKey{}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot verify requested audio") {
		t.Fatalf("silently accepted unverifiable audio: %v", err)
	}
}
