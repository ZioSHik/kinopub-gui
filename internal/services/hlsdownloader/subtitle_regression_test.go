package hlsdownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/downloader"
)

func TestSubtitlePlaylistHTTPFailuresRemainRetryable(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			d := newTestDownloader(t, srv.Client())
			master := &MasterPlaylist{Subtitles: []SubtitleRendition{{GroupID: "s", Name: "English", URI: srv.URL + "/sub.m3u8"}}}
			tracks, err := d.downloadSubtitleRenditions(context.Background(), master, Variant{SubtitleGroup: "s"}, t.TempDir())
			if err == nil || !isTransientSubtitleError(err) {
				t.Fatalf("HTTP %d must remain retryable, got tracks=%v err=%v", status, tracks, err)
			}
		})
	}
}

func TestWebVTTMergerImplicitOriginBeforeExplicitMap(t *testing.T) {
	var merger webVTTMerger
	for _, text := range []string{
		"WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nFirst\n",
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:900000\n\n00:00:01.000 --> 00:00:02.000\nSecond\n",
	} {
		if err := merger.add(text); err != nil {
			t.Fatal(err)
		}
	}
	if merger.mapBase != 0 || !strings.Contains(merger.body.String(), "00:00:11.000 --> 00:00:12.000") {
		t.Fatalf("implicit MPEGTS-zero origin was lost: base=%d cues=%s", merger.mapBase, merger.body.String())
	}
}

// Exercise downloading, timestamp-map propagation and actual container
// timestamps together. Normal unit tests still run when the tools are absent.
func TestHLSSubtitleMuxSynchronizesWithVideo(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is unavailable")
	}
	ctx := context.Background()
	video := filepath.Join(t.TempDir(), "video.ts")
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=160x90:r=25", "-t", "3", "-c:v", "mpeg2video", "-bf", "0", "-output_ts_offset", "10", "-muxdelay", "0", "-f", "mpegts", video}
	if out, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("create video: %v: %s", err, out)
	}
	videoData, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, header, cue string }{
		{"nonzero local", "X-TIMESTAMP-MAP=LOCAL:00:00:10.000,MPEGTS:900000\n", "00:00:11.000 --> 00:00:12.000"},
		{"zero local", "X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:900000\n", "00:00:01.000 --> 00:00:02.000"},
		{"map outside segment", "X-TIMESTAMP-MAP=LOCAL:00:00:05.000,MPEGTS:1350000\n", "00:00:01.000 --> 00:00:02.000"},
		{"implicit map", "", "00:00:11.000 --> 00:00:12.000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := hlsTestServer(t, map[string]string{
				"/master.m3u8": "#EXTM3U\n#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"s\",NAME=\"English\",LANGUAGE=\"eng\",URI=\"/sub.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,SUBTITLES=\"s\"\n/video.m3u8\n",
				"/video.m3u8":  "#EXTM3U\n#EXTINF:3,\n/video.ts\n#EXT-X-ENDLIST\n",
				"/video.ts":    string(videoData),
				"/sub.m3u8":    "#EXTM3U\n#EXTINF:3,\n/sub.vtt\n#EXT-X-ENDLIST\n",
				"/sub.vtt":     "WEBVTT\n" + tc.header + "\n" + tc.cue + "\nHello\n",
			})
			dir := t.TempDir()
			hls, err := newTestDownloader(t, srv.Client()).DownloadEpisode(ctx, srv.URL+"/master.m3u8", "720p", filepath.Join(dir, "download"), domain.EpisodeKey{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			run := func(ctx context.Context, name string, args, env []string, stdout io.Writer) error {
				cmd := exec.CommandContext(ctx, name, args...)
				cmd.Stdout = stdout
				var stderr strings.Builder
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("%w: %s", err, stderr.String())
				}
				return nil
			}
			output := filepath.Join(dir, "out.mkv")
			mux := downloader.New(run, nil, nopLogger{}, downloader.WithFFmpegPath(ffmpeg))
			if err := mux.MuxHLS(ctx, domain.Job{OutPath: output}, hls); err != nil {
				t.Fatal(err)
			}
			probe, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "s:0", "-show_packets", "-show_entries", "packet=pts_time", "-of", "json", output).Output()
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Packets []struct {
					PTS string `json:"pts_time"`
				} `json:"packets"`
			}
			if err := json.Unmarshal(probe, &got); err != nil || len(got.Packets) != 1 {
				t.Fatalf("probe: %s err=%v", probe, err)
			}
			pts, err := strconv.ParseFloat(got.Packets[0].PTS, 64)
			if err != nil || pts < 0.9 || pts > 1.1 {
				t.Fatalf("expected subtitle at 1s of output video, got %s", got.Packets[0].PTS)
			}
		})
	}
}
