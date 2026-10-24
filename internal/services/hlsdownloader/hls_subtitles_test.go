package hlsdownloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestDownloadEpisodeKeepsHLSSubtitlesWithoutAPI(t *testing.T) {
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8": `#EXTM3U
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="sub",NAME="English",LANGUAGE="eng",FORCED=YES,URI="/sub.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="unused",NAME="Other",URI="/missing.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,SUBTITLES="sub"
/video.m3u8
`,
		"/video.m3u8": "#EXTM3U\n#EXTINF:1200,\n/video.ts\n#EXT-X-ENDLIST\n",
		"/video.ts":   "video",
		"/sub.m3u8":   "#EXTM3U\n#EXTINF:600,\n/one.vtt\n#EXTINF:600,\n/two.vtt\n#EXT-X-ENDLIST\n",
		"/one.vtt":    "WEBVTT\n\n00:09:59.000 --> 00:10:01.000\nBoundary\n\n",
		"/two.vtt":    "WEBVTT\n\n00:09:59.000 --> 00:10:01.000\nBoundary\n\n00:10:02.000 --> 00:10:03.000\nLater\n\n",
	})
	defer srv.Close()
	d := newTestDownloader(t, srv.Client())
	result, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", filepath.Join(t.TempDir(), "video.ts"), domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SubtitleTracks) != 1 {
		t.Fatalf("lost HLS subtitles: %+v", result)
	}
	sub := result.SubtitleTracks[0]
	if sub.Language != "eng" || !sub.Forced {
		t.Fatalf("lost metadata: %+v", sub)
	}
	data, err := os.ReadFile(sub.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "Boundary") != 1 || !strings.Contains(text, "00:10:02.000 --> 00:10:03.000") || strings.Count(text, "WEBVTT") != 1 {
		t.Fatalf("incorrect merged cues: %s", text)
	}
}

func TestDownloadEpisodeSkipsBrokenHLSSubtitle(t *testing.T) {
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8": `#EXTM3U
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="sub",NAME="English",LANGUAGE="eng",URI="/sub.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="sub",NAME="Russian",LANGUAGE="rus",URI="/missing.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,SUBTITLES="sub"
/video.m3u8
`,
		"/video.m3u8": "#EXTM3U\n#EXTINF:10,\n/video.ts\n#EXT-X-ENDLIST\n",
		"/video.ts":   "video",
		"/sub.m3u8":   "#EXTM3U\n#EXTINF:10,\n/one.vtt\n#EXT-X-ENDLIST\n",
		"/one.vtt":    "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello\n\n",
	})
	d := newTestDownloader(t, srv.Client())
	result, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", filepath.Join(t.TempDir(), "video.ts"), domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatalf("a 404 subtitle rendition must not fail the downloaded episode: %v", err)
	}
	if len(result.SubtitleTracks) != 1 || result.SubtitleTracks[0].Language != "eng" {
		t.Fatalf("want only the working English track, got %+v", result.SubtitleTracks)
	}
}

func TestWebVTTMergerTimestampMaps(t *testing.T) {
	var merger webVTTMerger
	for _, text := range []string{
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:900000\n\n00:00:01.000 --> 00:00:02.000\nFirst\n",
		"WEBVTT\nX-TIMESTAMP-MAP=MPEGTS:54900000,LOCAL:00:00:00.000\n\n00:00:01.000 --> 00:00:02.000\nSecond\n",
	} {
		if err := merger.add(text); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(merger.body.String(), "00:10:01.000 --> 00:10:02.000") {
		t.Fatalf("timestamp map ignored: %s", merger.body.String())
	}
}

func TestWebVTTMergerRejectsInvalidSegments(t *testing.T) {
	for _, text := range []string{"<html>error</html>", "WEBVTT\n\ninvalid --> 00:00:02.000\nText\n"} {
		var merger webVTTMerger
		if merger.add(text) == nil {
			t.Fatalf("accepted malformed subtitles: %q", text)
		}
	}
}
