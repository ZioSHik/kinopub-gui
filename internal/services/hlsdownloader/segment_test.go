package hlsdownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// byteOnlySink implements ByteProgressSink but NOT SegmentProgressSink, to
// exercise the else-branch in updateTrack that falls back to ByteProgress.
type byteOnlySink struct {
	mu             sync.Mutex
	calls          int
	lastDownloaded int64
	lastTotal      int64
}

func (s *byteOnlySink) TrackProgress(domain.EpisodeKey, domain.TrackRef, int) {}
func (s *byteOnlySink) ByteProgress(_ domain.EpisodeKey, downloaded, total int64) {
	s.mu.Lock()
	s.calls++
	s.lastDownloaded = downloaded
	s.lastTotal = total
	s.mu.Unlock()
}

func TestDownloadEpisode_ByteProgressSinkFallback(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,CODECS="avc1.4d401f"
/720/media.m3u8
`
	media := "#EXTM3U\n#EXTINF:6.0,\n/720/s0.ts\n#EXTINF:6.0,\n/720/s1.ts\n#EXT-X-ENDLIST\n"
	srv := hlsTestServer(t, map[string]string{
		"/master.m3u8":    master,
		"/720/media.m3u8": media,
		"/720/s0.ts":      "AAAA",
		"/720/s1.ts":      "BBBB",
	})
	d := newTestDownloader(t, srv.Client())

	sink := &byteOnlySink{}
	res, err := d.DownloadEpisode(context.Background(), srv.URL+"/master.m3u8", "720p", filepath.Join(t.TempDir(), "ep.ts"), domain.EpisodeKey{}, sink)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.RemoveAll(res.TempDir)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.calls == 0 {
		t.Fatal("ByteProgress was never called")
	}
	// Final downloaded bytes should equal total video bytes (8).
	if sink.lastDownloaded != 8 {
		t.Errorf("final downloaded = %d, want 8", sink.lastDownloaded)
	}
	// With a single track fully downloaded, approx total should equal downloaded.
	if sink.lastTotal != 8 {
		t.Errorf("final approx total = %d, want 8", sink.lastTotal)
	}
}

// --- fetchSegment direct -------------------------------------------------

func TestFetchSegment_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "PAYLOAD")
	}))
	defer srv.Close()

	d := newTestDownloader(t, srv.Client())
	out := filepath.Join(t.TempDir(), "seg.ts")
	n, err := d.fetchSegment(context.Background(), srv.URL+"/seg.ts", out)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 7 {
		t.Errorf("n = %d, want 7", n)
	}
	b, _ := os.ReadFile(out)
	if string(b) != "PAYLOAD" {
		t.Errorf("file content = %q", string(b))
	}
}

func TestFetchSegment_HTTPErrorNoFileLeft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	d := newTestDownloader(t, srv.Client())
	out := filepath.Join(t.TempDir(), "seg.ts")
	_, err := d.fetchSegment(context.Background(), srv.URL+"/missing.ts", out)
	if err == nil {
		t.Fatal("expected an error for HTTP 404")
	}
	// On HTTP error the file must not have been created.
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("file should not exist after HTTP error, stat err = %v", statErr)
	}
}

func TestFetchSegment_BadURL(t *testing.T) {
	d := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{})
	d.client = http.DefaultClient
	_, err := d.fetchSegment(context.Background(), "://bad-url", filepath.Join(t.TempDir(), "x.ts"))
	if err == nil {
		t.Fatal("expected an error for malformed URL")
	}
}

// --- concatenateSegmentsDir ----------------------------------------------

func TestConcatenateSegmentsDir_MissingSegmentErrors(t *testing.T) {
	d := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{})
	segDir := t.TempDir()
	// Only seg 0 exists; seg 1 is missing.
	if err := os.WriteFile(filepath.Join(segDir, "seg_00000.ts"), []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	segs := []Segment{{Index: 0}, {Index: 1}}
	out := filepath.Join(t.TempDir(), "out.ts")
	err := d.concatenateSegmentsDir("", segs, segDir, out)
	if err == nil {
		t.Fatal("expected an error for missing segment file")
	}
}

func TestConcatenateSegmentsDir_OrderAndInit(t *testing.T) {
	d := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{})
	segDir := t.TempDir()
	// Write segments out of natural disk order to prove iteration uses the
	// segments slice ordering, not directory listing.
	for idx, body := range map[int]string{0: "AAA", 1: "BBB", 2: "CCC"} {
		if err := os.WriteFile(filepath.Join(segDir, fmt.Sprintf("seg_%05d.ts", idx)), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	initPath := filepath.Join(segDir, "init.mp4")
	if err := os.WriteFile(initPath, []byte("INIT"), 0644); err != nil {
		t.Fatal(err)
	}
	segs := []Segment{{Index: 0}, {Index: 1}, {Index: 2}}
	out := filepath.Join(t.TempDir(), "out.ts")
	if err := d.concatenateSegmentsDir(initPath, segs, segDir, out); err != nil {
		t.Fatalf("concat: %v", err)
	}
	b, _ := os.ReadFile(out)
	if string(b) != "INITAAABBBCCC" {
		t.Errorf("content = %q, want INITAAABBBCCC", string(b))
	}
}

func TestConcatenateSegmentsDir_MissingInitErrors(t *testing.T) {
	d := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{})
	segDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "out.ts")
	err := d.concatenateSegmentsDir(filepath.Join(segDir, "nonexistent-init.mp4"), nil, segDir, out)
	if err == nil {
		t.Fatal("expected an error when init segment is missing")
	}
}

// --- WithProxy -----------------------------------------------------------

func TestWithProxy(t *testing.T) {
	pu, _ := url.Parse("http://127.0.0.1:8080")
	d := New(http.DefaultClient, domain.RequestAuth{}, nopLogger{}, WithProxy(pu))
	if d.proxyURL == nil || d.proxyURL.Host != "127.0.0.1:8080" {
		t.Errorf("proxyURL = %v, want the configured proxy", d.proxyURL)
	}
}
