package hlsdownloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/downloader"
)

func TestAddingAudioPreservesVideoAudioAndSubtitles(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := t.TempDir()
	synth := func(name string, args ...string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		args = append([]string{"-v", "error", "-y"}, args...)
		args = append(args, path)
		if output, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
			t.Fatalf("generate %s: %v %s", name, err, output)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	video := synth("video.ts", "-f", "lavfi", "-i", "color=c=black:s=1280x720:r=25", "-t", "0.4", "-c:v", "mpeg2video", "-bf", "0", "-f", "mpegts")
	a := synth("a.aac", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "0.4", "-c:a", "aac", "-f", "adts")
	b := synth("b.aac", "-f", "lavfi", "-i", "sine=frequency=880", "-t", "0.4", "-c:a", "aac", "-f", "adts")
	var videoHits atomic.Int32
	files := map[string]string{
		"/master.m3u8": "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Studio A\",LANGUAGE=\"rus\",URI=\"/a.m3u8\"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Studio B\",LANGUAGE=\"eng\",URI=\"/b.m3u8\"\n#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"s\",NAME=\"English\",LANGUAGE=\"eng\",URI=\"/s.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,AUDIO=\"a\",SUBTITLES=\"s\"\n/video.m3u8\n",
		"/video.m3u8":  "#EXTM3U\n#EXTINF:0.4,\n/video.ts\n#EXT-X-ENDLIST\n",
		"/a.m3u8":      "#EXTM3U\n#EXTINF:0.4,\n/a.aac\n#EXT-X-ENDLIST\n",
		"/b.m3u8":      "#EXTM3U\n#EXTINF:0.4,\n/b.aac\n#EXT-X-ENDLIST\n",
		"/s.m3u8":      "#EXTM3U\n#EXTINF:0.4,\n/s.vtt\n#EXT-X-ENDLIST\n",
		"/video.ts":    video, "/a.aac": a, "/b.aac": b,
		"/s.vtt": "WEBVTT\n\n00:00:00.000 --> 00:00:00.300\nOriginal subtitle\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/video.ts" {
			videoHits.Add(1)
		}
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()
	run := func(ctx context.Context, name string, args, env []string, stdout io.Writer) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout = stdout
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%w: %s", err, stderr.String())
		}
		return nil
	}
	muxer := downloader.New(run, nil, nopLogger{}, downloader.WithFFmpegPath(ffmpeg))
	for _, ext := range []string{"mkv", "mp4"} {
		t.Run(ext, func(t *testing.T) {
			d := newTestDownloader(t, srv.Client())
			d.SetAudioPreference(domain.AudioPreference{Include: []string{"Studio A"}})
			path := filepath.Join(t.TempDir(), "episode."+ext)
			job := domain.Job{OutPath: path}
			r, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", path+".ts", domain.EpisodeKey{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := muxer.MuxHLS(context.Background(), job, r); err != nil {
				t.Fatal(err)
			}
			os.RemoveAll(r.TempDir)
			beforeHits := videoHits.Load()
			// Hash the encoded video packets; remuxing must not change their bytes.
			videoHash := func() string {
				t.Helper()
				out, err := exec.Command(ffmpeg, "-v", "error", "-i", path, "-map", "0:v:0", "-c", "copy", "-f", "hash", "-hash", "sha256", "-").CombinedOutput()
				if err != nil {
					t.Fatalf("video hash: %v %s", err, out)
				}
				return string(out)
			}
			hash := videoHash()
			d.SetAudioPreference(domain.AudioPreference{Include: []string{"Studio B"}})
			added, err := d.DownloadAudio(context.Background(), srv.URL+"/master.m3u8", "720p", path+".ts", domain.EpisodeKey{}, nil, []string{"Studio A"})
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(added.TempDir)
			if videoHits.Load() != beforeHits {
				t.Fatal("adding a dub downloaded video again")
			}
			added.VideoPath = path
			added.PreserveExisting = true
			if err := muxer.MuxHLS(context.Background(), job, added); err != nil {
				t.Fatal(err)
			}
			if videoHash() != hash {
				t.Fatal("adding audio changed the encoded video")
			}
			output, err := exec.Command(ffprobe, "-v", "error", "-show_streams", "-of", "json", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			var probe struct {
				Streams []struct {
					CodecType string            `json:"codec_type"`
					Tags      map[string]string `json:"tags"`
				} `json:"streams"`
			}
			if err := json.Unmarshal(output, &probe); err != nil {
				t.Fatal(err)
			}
			counts := map[string]int{}
			var labels []string
			for _, stream := range probe.Streams {
				counts[stream.CodecType]++
				if stream.CodecType == "audio" {
					labels = append(labels, stream.Tags["title"])
				}
			}
			if counts["video"] != 1 || counts["audio"] != 2 || counts["subtitle"] != 1 {
				t.Fatalf("lost streams: %s", output)
			}
			if ext == "mkv" && strings.Join(labels, ",") != "Studio A,Studio B" {
				t.Fatalf("wrong audio titles: %v", labels)
			}
		})
	}
}

func TestSelectedAudioPlaylistFailureIsAnError(t *testing.T) {
	for _, pref := range []domain.AudioPreference{{}, {Include: []string{"Studio A"}}} {
		srv := hlsTestServer(t, map[string]string{
			"/master.m3u8": "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"Studio A\",URI=\"/a.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,AUDIO=\"a\"\n/v.m3u8\n",
			"/v.m3u8":      "#EXTM3U\n#EXTINF:1,\n/v.ts\n#EXT-X-ENDLIST\n",
			"/a.m3u8":      "#EXTM3U\n#EXTINF:invalid,\n/a.ts\n",
		})
		d := newTestDownloader(t, srv.Client())
		d.SetAudioPreference(pref)
		if _, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", filepath.Join(t.TempDir(), "v.ts"), domain.EpisodeKey{}, nil); err == nil {
			t.Fatal("selected audio silently dropped")
		}
	}
}
