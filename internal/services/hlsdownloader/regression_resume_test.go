package hlsdownloader

import (
	"context"
	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func TestResumeDoesNotMixDifferentVariants(t *testing.T) {
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,CODECS=\"avc1\"\n/720.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1920x1080,CODECS=\"avc1\"\n/1080.m3u8\n",
		"/720.m3u8":    "#EXTM3U\n#EXTINF:3,\n/720.ts\n#EXT-X-ENDLIST\n",
		"/1080.m3u8":   "#EXTM3U\n#EXTINF:3,\n/1080.ts\n#EXT-X-ENDLIST\n",
		"/720.ts":      "720p payload", "/1080.ts": "1080p payload",
	})
	d := newTestDownloader(t, srv.Client())
	out := filepath.Join(t.TempDir(), "video.ts")
	if _, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", out, domain.EpisodeKey{}, nil); err != nil {
		t.Fatal(err)
	}
	// Leave the cache, as it would remain after a pause or an abrupt exit.
	r, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "1080p", out, domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(r.VideoPath)
	if err != nil || string(data) != "1080p payload" {
		t.Fatalf("selected=%s, downloaded bytes=%q err=%v", r.Resolution, data, err)
	}
}
