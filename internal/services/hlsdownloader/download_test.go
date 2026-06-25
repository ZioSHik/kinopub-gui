package hlsdownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// --- test logger ---------------------------------------------------------

type nopLogger struct{}

func (nopLogger) Debug(string, ...domain.Field)      {}
func (nopLogger) Info(string, ...domain.Field)       {}
func (nopLogger) Warn(string, ...domain.Field)       {}
func (nopLogger) Error(string, ...domain.Field)      {}
func (nopLogger) With(...domain.Field) domain.Logger { return nopLogger{} }
func (nopLogger) Component(string) domain.Logger     { return nopLogger{} }

// --- recording progress sink (implements all sub-interfaces) -------------

type recordingSink struct {
	mu            sync.Mutex
	lastPct       int
	hlsCalls      int
	lastHLSTracks []domain.TrackProgressInfo
	segCalls      int
	lastDone      int
	lastTotal     int
	lastBytes     int64
	lastApproxTot int64
}

func (s *recordingSink) TrackProgress(_ domain.EpisodeKey, _ domain.TrackRef, pct int) {
	s.mu.Lock()
	s.lastPct = pct
	s.mu.Unlock()
}

func (s *recordingSink) HLSProgress(_ domain.EpisodeKey, tracks []domain.TrackProgressInfo) {
	s.mu.Lock()
	s.hlsCalls++
	s.lastHLSTracks = tracks
	s.mu.Unlock()
}

func (s *recordingSink) SegmentProgress(_ domain.EpisodeKey, done, total int, bytes, approx int64) {
	s.mu.Lock()
	s.segCalls++
	s.lastDone = done
	s.lastTotal = total
	s.lastBytes = bytes
	s.lastApproxTot = approx
	s.mu.Unlock()
}

// newTestDownloader builds a Downloader wired to the given client (so httptest
// works without the uTLS browser transport).
func newTestDownloader(t *testing.T, client *http.Client, opts ...Option) *Downloader {
	t.Helper()
	d := New(client, domain.RequestAuth{}, nopLogger{}, opts...)
	d.client = client // override the browser client New() installed
	return d
}

// hlsTestServer serves a configurable set of m3u8 playlists and .ts/.m4s
// segments. Paths map to bodies. Segment bodies are returned verbatim.
func hlsTestServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, body := range files {
		body := body
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// --- FetchMasterPlaylist / FetchMediaPlaylist ----------------------------

func TestFetchMasterPlaylist_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f"
720/media.m3u8
`)
	}))
	defer srv.Close()

	master, err := FetchMasterPlaylist(context.Background(), srv.Client(), srv.URL+"/master.m3u8", domain.RequestAuth{}, nopLogger{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(master.Variants) != 1 || master.Variants[0].Height != 720 {
		t.Fatalf("unexpected variants: %+v", master.Variants)
	}
}

func TestFetchMasterPlaylist_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := FetchMasterPlaylist(context.Background(), srv.Client(), srv.URL+"/master.m3u8", domain.RequestAuth{}, nopLogger{})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v, want HTTP 403", err)
	}
}

func TestFetchMasterPlaylist_SendsAuthHeaders(t *testing.T) {
	var gotUA, gotCookie, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotCookie = r.Header.Get("Cookie")
		gotCustom = r.Header.Get("X-Custom")
		fmt.Fprint(w, "#EXTM3U\n")
	}))
	defer srv.Close()

	auth := domain.RequestAuth{
		UserAgent: "TestAgent/1.0",
		Cookie:    "cf_clearance=abc",
		Headers:   map[string]string{"X-Custom": "yes"},
	}
	if _, err := FetchMasterPlaylist(context.Background(), srv.Client(), srv.URL+"/m.m3u8", auth, nopLogger{}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotUA != "TestAgent/1.0" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotCookie != "cf_clearance=abc" {
		t.Errorf("Cookie = %q", gotCookie)
	}
	if gotCustom != "yes" {
		t.Errorf("X-Custom = %q", gotCustom)
	}
}

func TestFetchMediaPlaylist_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXTINF:6.0,\nseg_0.ts\n#EXTINF:6.0,\nseg_1.ts\n")
	}))
	defer srv.Close()

	pl, err := FetchMediaPlaylist(context.Background(), srv.Client(), srv.URL+"/media.m3u8", domain.RequestAuth{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pl.Segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(pl.Segments))
	}
}

func TestFetchMediaPlaylist_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := FetchMediaPlaylist(ctx, http.DefaultClient, "https://example.invalid/m.m3u8", domain.RequestAuth{})
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

// --- formatHLSBytes ------------------------------------------------------

func TestFormatHLSBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0.0 KB"},
		{512, "0.5 KB"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{3 * 1024 * 1024 * 1024 / 2, "1.5 GB"},
	}
	for _, tt := range tests {
		if got := formatHLSBytes(tt.in); got != tt.want {
			t.Errorf("formatHLSBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- ListAudioTracks -----------------------------------------------------

func TestListAudioTracks(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="RUS Dub",LANGUAGE="rus",URI="/audio/rus.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="ENG",LANGUAGE="eng",URI="/audio/eng.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f",AUDIO="aud"
/720/media.m3u8
`
	srv := hlsTestServer(t, map[string]string{"/master.m3u8": master})
	d := newTestDownloader(t, srv.Client())

	infos, err := d.ListAudioTracks(context.Background(), srv.URL+"/master.m3u8", "720p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("got %d audio tracks, want 2", len(infos))
	}
	if infos[0].Name != "RUS Dub" || infos[0].Language != "rus" || infos[0].Index != 0 {
		t.Errorf("infos[0] = %+v", infos[0])
	}
	if infos[1].Index != 1 || infos[1].Name != "ENG" {
		t.Errorf("infos[1] = %+v", infos[1])
	}
}

func TestListAudioTracks_NoVariants(t *testing.T) {
	srv := hlsTestServer(t, map[string]string{"/master.m3u8": "#EXTM3U\n"})
	d := newTestDownloader(t, srv.Client())
	_, err := d.ListAudioTracks(context.Background(), srv.URL+"/master.m3u8", "")
	if err == nil || !strings.Contains(err.Error(), "no variants") {
		t.Fatalf("err = %v, want no-variants error", err)
	}
}

// --- DownloadEpisode end-to-end ------------------------------------------

func TestDownloadEpisode_VideoOnly_TS(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f"
/720/media.m3u8
`
	media := `#EXTM3U
#EXT-X-VERSION:3
#EXTINF:6.0,
/720/seg_0.ts
#EXTINF:6.0,
/720/seg_1.ts
#EXTINF:6.0,
/720/seg_2.ts
#EXT-X-ENDLIST
`
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8":    master,
		"/720/media.m3u8": media,
		"/720/seg_0.ts":   "AAAA",
		"/720/seg_1.ts":   "BBBB",
		"/720/seg_2.ts":   "CCCC",
	})
	d := newTestDownloader(t, srv.Client())

	outPath := filepath.Join(t.TempDir(), "ep.ts")
	sink := &recordingSink{}
	res, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", outPath, domain.EpisodeKey{Season: 1, Episode: 2}, sink)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	if res.Resolution != "1280x720" {
		t.Errorf("Resolution = %q", res.Resolution)
	}
	if res.BitrateKbps != 2400 {
		t.Errorf("BitrateKbps = %d, want 2400", res.BitrateKbps)
	}
	if res.Codec != "h264" {
		t.Errorf("Codec = %q, want h264", res.Codec)
	}
	if len(res.AudioTracks) != 0 {
		t.Errorf("AudioTracks = %v, want none (muxed)", res.AudioTracks)
	}
	// 3 segments * 4 bytes = 12 bytes.
	if res.TotalBytes != 12 {
		t.Errorf("TotalBytes = %d, want 12", res.TotalBytes)
	}

	// Concatenated video file should be AAAABBBBCCCC in order.
	got, err := os.ReadFile(res.VideoPath)
	if err != nil {
		t.Fatalf("read video: %v", err)
	}
	if string(got) != "AAAABBBBCCCC" {
		t.Errorf("video content = %q, want ordered concat AAAABBBBCCCC", string(got))
	}

	// Progress sink: final percent should reach 100.
	if sink.lastPct != 100 {
		t.Errorf("final pct = %d, want 100", sink.lastPct)
	}
	if sink.lastDone != 3 || sink.lastTotal != 3 {
		t.Errorf("segment progress done/total = %d/%d, want 3/3", sink.lastDone, sink.lastTotal)
	}
	if sink.hlsCalls == 0 {
		t.Error("HLSProgress was never called")
	}
}

func TestDownloadEpisode_WithAudioTracks(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="RUS",LANGUAGE="rus",URI="/audio/rus.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="ENG",LANGUAGE="eng",URI="/audio/eng.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f",AUDIO="aud"
/720/media.m3u8
`
	videoMedia := `#EXTM3U
#EXTINF:6.0,
/720/v0.ts
#EXTINF:6.0,
/720/v1.ts
#EXT-X-ENDLIST
`
	audioMedia := `#EXTM3U
#EXTINF:6.0,
/audio/a0.ts
#EXT-X-ENDLIST
`
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8":    master,
		"/720/media.m3u8": videoMedia,
		"/audio/rus.m3u8": audioMedia,
		"/audio/eng.m3u8": audioMedia,
		"/720/v0.ts":      "V0",
		"/720/v1.ts":      "V1",
		"/audio/a0.ts":    "AUDIODATA",
	})
	d := newTestDownloader(t, srv.Client())

	outPath := filepath.Join(t.TempDir(), "ep.ts")
	res, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", outPath, domain.EpisodeKey{Season: 1, Episode: 1}, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	if len(res.AudioTracks) != 2 {
		t.Fatalf("got %d audio tracks, want 2", len(res.AudioTracks))
	}
	// Audio result order must match rendition order.
	names := []string{res.AudioTracks[0].Name, res.AudioTracks[1].Name}
	sort.Strings(names)
	if names[0] != "ENG" || names[1] != "RUS" {
		t.Errorf("audio names = %v, want [ENG RUS]", names)
	}
	for _, at := range res.AudioTracks {
		b, err := os.ReadFile(at.Path)
		if err != nil {
			t.Fatalf("read audio %q: %v", at.Name, err)
		}
		if string(b) != "AUDIODATA" {
			t.Errorf("audio %q content = %q", at.Name, string(b))
		}
	}
	// TotalBytes = video (2+2) + 2 audio tracks * 9 = 4 + 18 = 22.
	if res.TotalBytes != 22 {
		t.Errorf("TotalBytes = %d, want 22", res.TotalBytes)
	}
}

func TestDownloadEpisode_InitSegmentPrependedFMP4(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=12000000,RESOLUTION=3840x2160,CODECS="hvc1.2.4.L150"
/4k/media.m3u8
`
	media := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-MAP:URI="/4k/init.mp4"
#EXTINF:6.0,
/4k/seg_0.m4s
#EXTINF:6.0,
/4k/seg_1.m4s
#EXT-X-ENDLIST
`
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8":   master,
		"/4k/media.m3u8": media,
		"/4k/init.mp4":   "INIT",
		"/4k/seg_0.m4s":  "S0",
		"/4k/seg_1.m4s":  "S1",
	})
	d := newTestDownloader(t, srv.Client())

	outPath := filepath.Join(t.TempDir(), "ep.ts")
	res, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "max", outPath, domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	if res.Codec != "h265" {
		t.Errorf("Codec = %q, want h265", res.Codec)
	}
	got, err := os.ReadFile(res.VideoPath)
	if err != nil {
		t.Fatalf("read video: %v", err)
	}
	// init segment must be prepended before the media fragments.
	if string(got) != "INITS0S1" {
		t.Errorf("video content = %q, want INITS0S1 (init prepended)", string(got))
	}
}

func TestDownloadEpisode_ResumeSkipsExistingSegments(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f"
/720/media.m3u8
`
	media := `#EXTM3U
#EXTINF:6.0,
/720/seg_0.ts
#EXTINF:6.0,
/720/seg_1.ts
#EXT-X-ENDLIST
`
	var seg0Hits int
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, master) })
	mux.HandleFunc("/720/media.m3u8", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, media) })
	mux.HandleFunc("/720/seg_0.ts", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seg0Hits++
		mu.Unlock()
		fmt.Fprint(w, "SEG0")
	})
	mux.HandleFunc("/720/seg_1.ts", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "SEG1") })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d := newTestDownloader(t, srv.Client())
	outPath := filepath.Join(t.TempDir(), "ep.ts")

	// Pre-create the temp dir + a finished seg_0 so resume can skip it.
	segDir := filepath.Join(outPath+".hls-tmp", "video")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(segDir, "seg_00000.ts"), []byte("RESUMED!"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", outPath, domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	mu.Lock()
	hits := seg0Hits
	mu.Unlock()
	if hits != 0 {
		t.Errorf("seg_0 was fetched %d times, want 0 (resume should skip it)", hits)
	}
	got, _ := os.ReadFile(res.VideoPath)
	if string(got) != "RESUMED!SEG1" {
		t.Errorf("video content = %q, want RESUMED!SEG1", string(got))
	}
}

func TestDownloadEpisode_NoVariantsError(t *testing.T) {
	srv := hlsTestServer(t, map[string]string{"/master.m3u8": "#EXTM3U\n"})
	d := newTestDownloader(t, srv.Client())
	_, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "", filepath.Join(t.TempDir(), "x.ts"), domain.EpisodeKey{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no variants") {
		t.Fatalf("err = %v, want no-variants", err)
	}
}

func TestDownloadEpisode_NoSegmentsError(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f"
/720/media.m3u8
`
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8":    master,
		"/720/media.m3u8": "#EXTM3U\n#EXT-X-ENDLIST\n",
	})
	d := newTestDownloader(t, srv.Client())
	_, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", filepath.Join(t.TempDir(), "x.ts"), domain.EpisodeKey{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no segments") {
		t.Fatalf("err = %v, want no-segments", err)
	}
}

func TestDownloadEpisode_AudioPreferenceFilters(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="RUS Dub",LANGUAGE="rus",URI="/audio/rus.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="ENG Original",LANGUAGE="eng",URI="/audio/eng.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f",AUDIO="aud"
/720/media.m3u8
`
	videoMedia := "#EXTM3U\n#EXTINF:6.0,\n/720/v0.ts\n#EXT-X-ENDLIST\n"
	audioMedia := "#EXTM3U\n#EXTINF:6.0,\n/audio/a0.ts\n#EXT-X-ENDLIST\n"
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8":    master,
		"/720/media.m3u8": videoMedia,
		"/audio/rus.m3u8": audioMedia,
		"/audio/eng.m3u8": audioMedia,
		"/720/v0.ts":      "V",
		"/audio/a0.ts":    "A",
	})
	d := newTestDownloader(t, srv.Client())
	d.SetAudioPreference(domain.AudioPreference{Include: []string{"rus"}})

	outPath := filepath.Join(t.TempDir(), "ep.ts")
	res, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", outPath, domain.EpisodeKey{}, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	if len(res.AudioTracks) != 1 {
		t.Fatalf("got %d audio tracks, want 1 (filtered to RUS)", len(res.AudioTracks))
	}
	if res.AudioTracks[0].Language != "rus" {
		t.Errorf("kept audio = %+v, want rus", res.AudioTracks[0])
	}
}

// --- SetAudioPreference / audioPreference round-trip ---------------------

func TestAudioPreferenceRoundTrip(t *testing.T) {
	d := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{})
	if !d.audioPreference().IsAll() {
		t.Error("default audio preference should be all")
	}
	pref := domain.AudioPreference{Include: []string{"x"}}
	d.SetAudioPreference(pref)
	if d.audioPreference().IsAll() {
		t.Error("after Set, IsAll should be false")
	}
}

// --- WithConcurrency / WithProxy options ---------------------------------

func TestWithConcurrency(t *testing.T) {
	d := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{}, WithConcurrency(10))
	if d.concurrency != 10 {
		t.Errorf("concurrency = %d, want 10", d.concurrency)
	}
	// Values < 1 are ignored, keeping the default.
	d2 := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{}, WithConcurrency(0))
	if d2.concurrency != defaultConcurrency {
		t.Errorf("concurrency = %d, want default %d", d2.concurrency, defaultConcurrency)
	}
}
