package gui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

type auditRoundTripper func(*http.Request) (*http.Response, error)

func (f auditRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the actual HTTP admission, API scraper, HLS selection, ffmpeg,
// completion state and library as one workflow, without contacting a real account.
func TestDownloadWorkflowEndToEnd(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	fixtures := t.TempDir()
	synth := func(name string, args ...string) string {
		t.Helper()
		path := filepath.Join(fixtures, name)
		argv := append([]string{"-v", "error", "-y"}, args...)
		argv = append(argv, path)
		if out, err := exec.Command(ffmpeg, argv...).CombinedOutput(); err != nil {
			t.Fatalf("generate fixture: %v %s", err, out)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	video720 := synth("720.ts", "-f", "lavfi", "-i", "color=c=black:s=1280x720:r=25", "-t", "0.4", "-c:v", "mpeg2video", "-bf", "0", "-f", "mpegts")
	video360 := synth("360.ts", "-f", "lavfi", "-i", "color=c=black:s=640x360:r=25", "-t", "0.4", "-c:v", "mpeg2video", "-bf", "0", "-f", "mpegts")
	audioA := synth("a.aac", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "0.4", "-ac", "2", "-c:a", "aac", "-f", "adts")
	audioB := synth("b.aac", "-f", "lavfi", "-i", "sine=frequency=880", "-t", "0.4", "-ac", "2", "-c:a", "aac", "-f", "adts")
	var videoHits atomic.Int32
	files := map[string]string{
		"/master.m3u8": "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"01. Дубляж. Studio A (RUS)\",LANGUAGE=\"rus\",URI=\"/a.m3u8\"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"02. Оригинал (ENG)\",LANGUAGE=\"eng\",URI=\"/b.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720,AUDIO=\"a\"\n/720.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360,AUDIO=\"a\"\n/360.m3u8\n",
		"/720.ts":      video720, "/360.ts": video360, "/a.aac": audioA, "/b.aac": audioB,
	}
	for _, name := range []string{"720", "360", "a", "b"} {
		ext := "ts"
		if name == "a" || name == "b" {
			ext = "aac"
		}
		files["/"+name+".m3u8"] = "#EXTM3U\n#EXTINF:0.4,\n/" + name + "." + ext + "\n#EXT-X-ENDLIST\n"
	}
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".ts") {
			videoHits.Add(1)
		}
		if data, ok := files[r.URL.Path]; ok {
			io.WriteString(w, data)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer media.Close()
	audios := []kinopubapi.Audio{{Index: 1, Lang: "rus", Type: kinopubapi.NamedID{Title: "Дубляж"}, Author: kinopubapi.NamedID{Title: "Studio A"}, Codec: "aac", Channels: 2}, {Index: 2, Lang: "eng", Type: kinopubapi.NamedID{Title: "Оригинал"}, Codec: "aac", Channels: 2}}
	item := kinopubapi.Item{ID: json.Number("42"), Type: "movie", Title: "Audit fixture", Videos: []kinopubapi.Video{{Number: 1, Title: "Audit fixture", Duration: 1, Audios: audios, Files: []kinopubapi.File{{Quality: "720p", H: 720, W: 1280, URL: kinopubapi.FileURL{HLS4: media.URL + "/master.m3u8"}}}}}}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/items/42" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"item": item})
	}))
	defer apiServer.Close()
	apiBase, _ := url.Parse(apiServer.URL)
	for _, container := range []string{"mkv", "mp4"} {
		t.Run(container, func(t *testing.T) {
			s := newTestServer(t)
			s.kpClientCached = kinopubapi.New(&http.Client{Transport: auditRoundTripper(func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				u := *r.URL
				u.Scheme, u.Host = apiBase.Scheme, apiBase.Host
				copy.URL = &u
				return http.DefaultTransport.RoundTrip(copy)
			})}, kinopubapi.Tokens{AccessToken: "fake-test-token", Expiry: time.Now().Add(time.Hour)})
			appServer := httptest.NewServer(s.Handler())
			defer appServer.Close()
			out := t.TempDir()
			sources := []domain.AudioSource{{Language: "rus", Type: "Дубляж", Author: "Studio A", Codec: "aac", Channels: 2}, {Language: "eng", Type: "Оригинал", Codec: "aac", Channels: 2}}
			submit := func(quality string, source domain.AudioSource) JobView {
				t.Helper()
				raw, _ := json.Marshal(StartRequest{RunRequest: RunRequest{URL: "https://kino.watch/item/view/42", OutputPath: out, Quality: quality, Container: container, FFmpegPath: ffmpeg, AudioSpecs: []AudioSpecDTO{{Require: []string{source.Type}, Source: &source}}}})
				response, err := http.Post(appServer.URL+"/api/jobs", "application/json", bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusAccepted {
					t.Fatalf("start %d: %s", response.StatusCode, data)
				}
				var view JobView
				if err := json.Unmarshal(data, &view); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(12 * time.Second)
				for time.Now().Before(deadline) {
					j, _ := s.mgr.get(view.ID)
					view = j.snapshot()
					if view.Status == statusCompleted {
						return view
					}
					if view.Status == statusFailed || view.Status == statusCanceled {
						t.Fatalf("download failed: %s", view.Error)
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("download did not finish: %s", view.Status)
				return view
			}
			scan := func() LibrarySeries {
				t.Helper()
				items := scanLibrary([]string{out}).Series
				if len(items) != 1 {
					t.Fatalf("library titles=%d", len(items))
				}
				return items[0]
			}
			submit("720p", sources[0])
			first := scan()
			if first.Count != 1 || len(first.Episodes) != 1 {
				t.Fatalf("library=%+v", first)
			}
			path := first.Episodes[0].Path
			hash := func() string {
				t.Helper()
				data, err := exec.Command(ffmpeg, "-v", "error", "-i", path, "-map", "0:v:0", "-c", "copy", "-f", "hash", "-hash", "sha256", "-").CombinedOutput()
				if err != nil {
					t.Fatalf("hash: %v %s", err, data)
				}
				return string(data)
			}
			beforeHash, beforeHits := hash(), videoHits.Load()
			submit("720p", sources[1])
			if videoHits.Load() != beforeHits || hash() != beforeHash {
				t.Fatal("adding audio downloaded or changed video")
			}
			output, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "a", "-show_entries", "stream=index", "-of", "json", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			var probe struct{ Streams []json.RawMessage }
			json.Unmarshal(output, &probe)
			if len(probe.Streams) != 2 {
				t.Fatalf("audio streams=%d", len(probe.Streams))
			}
			beforeHits = videoHits.Load()
			submit("720p", sources[1])
			if videoHits.Load() != beforeHits {
				t.Fatal("same completed rendition downloaded again")
			}
			submit("360p", sources[0])
			second := scan()
			if second.Count != 1 || len(second.Episodes) != 1 || len(second.Episodes[0].Versions) != 1 {
				t.Fatalf("new quality replaced logical episode or lost prior file: %+v", second)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("old video version disappeared")
			}
			newest := second.Episodes[0].Path
			if err := os.Remove(newest); err != nil {
				t.Fatal(err)
			}
			beforeHits = videoHits.Load()
			submit("360p", sources[0])
			if videoHits.Load() <= beforeHits {
				t.Fatal("missing version was not downloaded again")
			}
			s.mgr.persistNow()
			restored := newJobManager(newHub())
			restored.attachStore(s.mgr.store)
			if len(restored.list()) != len(s.mgr.list()) {
				t.Fatal("finished queue did not survive restart")
			}
		})
	}
}
