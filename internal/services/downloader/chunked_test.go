package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// byteSink records both percentage and byte progress updates.
type byteSink struct {
	mu    sync.Mutex
	pcts  []int
	bytes [][2]int64 // {downloaded, total}
}

func (s *byteSink) TrackProgress(_ domain.EpisodeKey, _ domain.TrackRef, percent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pcts = append(s.pcts, percent)
}

func (s *byteSink) ByteProgress(_ domain.EpisodeKey, downloaded, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytes = append(s.bytes, [2]int64{downloaded, total})
}

func (s *byteSink) lastByte() (int64, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bytes) == 0 {
		return 0, 0, false
	}
	last := s.bytes[len(s.bytes)-1]
	return last[0], last[1], true
}

func newTestChunked(t *testing.T, auth domain.RequestAuth) *ChunkedDownloader {
	t.Helper()
	return NewChunked(&http.Client{}, auth, testLogger{})
}

func TestChunked_CanHandle(t *testing.T) {
	c := newTestChunked(t, domain.RequestAuth{})
	tests := []struct {
		name  string
		media domain.ResolvedMedia
		want  bool
	}{
		{"progressive with url", domain.ResolvedMedia{Source: domain.MediaSource{Kind: domain.MediaProgressive, URL: "http://x/v.mp4"}}, true},
		{"progressive no url", domain.ResolvedMedia{Source: domain.MediaSource{Kind: domain.MediaProgressive}}, false},
		{"hls with url", domain.ResolvedMedia{Source: domain.MediaSource{Kind: domain.MediaHLS, URL: "http://x/m.m3u8"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.CanHandle(tt.media); got != tt.want {
				t.Errorf("CanHandle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewChunked_ClearsTimeout(t *testing.T) {
	client := &http.Client{Timeout: 99}
	c := NewChunked(client, domain.RequestAuth{}, testLogger{})
	if c.client.Timeout != 0 {
		t.Errorf("expected cleared timeout, got %v", c.client.Timeout)
	}
	// Original client untouched (we copied it).
	if client.Timeout != 99 {
		t.Errorf("original client timeout mutated: %v", client.Timeout)
	}
}

// rangeServer serves body with HEAD content-length and Range support.
func rangeServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			return
		}
		http.ServeContent(w, r, "v.mp4", testModTime, bytesReadSeeker(body))
	}))
}

func TestChunked_Download_FullFile(t *testing.T) {
	body := []byte(strings.Repeat("A", 1500*1024)) // 1.5MB so progress reports trigger
	srv := rangeServer(t, body)
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "v.mp4")
	sink := &byteSink{}
	c := newTestChunked(t, domain.RequestAuth{})

	err := c.Download(context.Background(), srv.URL, out, domain.EpisodeKey{Season: 1, Episode: 1}, sink)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(got) != len(body) {
		t.Fatalf("size mismatch: got %d, want %d", len(got), len(body))
	}
	// .part file should be gone (renamed).
	if _, err := os.Stat(out + ".part"); !os.IsNotExist(err) {
		t.Error(".part file should be removed after success")
	}
	// Final byte progress should be downloaded==total.
	d, total, ok := sink.lastByte()
	if !ok {
		t.Fatal("expected byte progress")
	}
	if d != int64(len(body)) || total != int64(len(body)) {
		t.Errorf("final byte progress = %d/%d, want %d/%d", d, total, len(body), len(body))
	}
	// Should have reached 100%.
	last := sink.pcts[len(sink.pcts)-1]
	if last != 100 {
		t.Errorf("final percent = %d, want 100", last)
	}
}

func TestChunked_Download_ResumesFromPartial(t *testing.T) {
	body := []byte(strings.Repeat("B", 2000))
	srv := rangeServer(t, body)
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "v.mp4")
	// Pre-create a .part with the first 800 bytes.
	if err := os.WriteFile(out+".part", body[:800], 0644); err != nil {
		t.Fatal(err)
	}

	sink := &byteSink{}
	c := newTestChunked(t, domain.RequestAuth{})
	err := c.Download(context.Background(), srv.URL, out, domain.EpisodeKey{Season: 1, Episode: 1}, sink)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("resumed content mismatch: got %d bytes", len(got))
	}
}

func TestChunked_Download_AlreadyComplete(t *testing.T) {
	body := []byte(strings.Repeat("C", 500))
	srv := rangeServer(t, body)
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "v.mp4")
	// .part already has the full file.
	if err := os.WriteFile(out+".part", body, 0644); err != nil {
		t.Fatal(err)
	}

	sink := &byteSink{}
	c := newTestChunked(t, domain.RequestAuth{})
	err := c.Download(context.Background(), srv.URL, out, domain.EpisodeKey{Season: 2, Episode: 3}, sink)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not created: %v", err)
	}
	// Should report 100% immediately.
	if len(sink.pcts) == 0 || sink.pcts[len(sink.pcts)-1] != 100 {
		t.Errorf("expected 100%% on already-complete, got %v", sink.pcts)
	}
}

func TestChunked_Download_OversizedPartialRestarts(t *testing.T) {
	body := []byte(strings.Repeat("D", 400))
	srv := rangeServer(t, body)
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "v.mp4")
	// .part is larger than the real file → must be discarded and restarted.
	if err := os.WriteFile(out+".part", []byte(strings.Repeat("X", 999)), 0644); err != nil {
		t.Fatal(err)
	}

	c := newTestChunked(t, domain.RequestAuth{})
	err := c.Download(context.Background(), srv.URL, out, domain.EpisodeKey{Season: 1, Episode: 1}, nil)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("expected fresh download of correct content, got %q", string(got))
	}
}

func TestChunked_Download_ProbeFailsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "v.mp4")
	c := newTestChunked(t, domain.RequestAuth{})
	err := c.Download(context.Background(), srv.URL, out, domain.EpisodeKey{}, nil)
	if err == nil {
		t.Fatal("expected probe error on 403")
	}
	if !strings.Contains(err.Error(), "probe") {
		t.Errorf("expected probe error, got %v", err)
	}
}

func TestChunked_getPartialOffset(t *testing.T) {
	dir := t.TempDir()
	c := newTestChunked(t, domain.RequestAuth{})

	// No file → 0.
	if got := c.getPartialOffset(filepath.Join(dir, "none.part"), 100); got != 0 {
		t.Errorf("missing file offset = %d, want 0", got)
	}

	// Normal partial smaller than total.
	p := filepath.Join(dir, "a.part")
	os.WriteFile(p, make([]byte, 50), 0644)
	if got := c.getPartialOffset(p, 100); got != 50 {
		t.Errorf("offset = %d, want 50", got)
	}

	// Oversized → deleted, returns 0.
	big := filepath.Join(dir, "b.part")
	os.WriteFile(big, make([]byte, 200), 0644)
	if got := c.getPartialOffset(big, 100); got != 0 {
		t.Errorf("oversized offset = %d, want 0", got)
	}
	if _, err := os.Stat(big); !os.IsNotExist(err) {
		t.Error("oversized partial should be deleted")
	}

	// Unknown total (0) → returns size without deleting.
	z := filepath.Join(dir, "c.part")
	os.WriteFile(z, make([]byte, 77), 0644)
	if got := c.getPartialOffset(z, 0); got != 77 {
		t.Errorf("unknown-total offset = %d, want 77", got)
	}
}

func TestChunked_probeSize_ContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.Header().Set("Content-Length", "4242")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestChunked(t, domain.RequestAuth{})
	size, err := c.probeSize(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("probeSize error: %v", err)
	}
	if size != 4242 {
		t.Errorf("size = %d, want 4242", size)
	}
}

func TestChunked_applyAuth_CookieOnlyForKinoPub(t *testing.T) {
	auth := domain.RequestAuth{
		UserAgent: "UA/1",
		Cookie:    "sid=1",
		Headers:   map[string]string{"Referer": "https://kino.pub/"},
	}
	c := newTestChunked(t, auth)

	t.Run("cdn host gets no cookie", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "https://cdntogo.net/v.mp4", nil)
		c.applyAuth(req)
		if req.Header.Get("Cookie") != "" {
			t.Error("CDN request must not carry Cookie")
		}
		if req.Header.Get("User-Agent") != "UA/1" {
			t.Error("expected User-Agent set")
		}
		if req.Header.Get("Referer") != "https://kino.pub/" {
			t.Error("expected Referer header set")
		}
	})
	t.Run("kino.pub host gets cookie", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "https://api.kino.pub/x", nil)
		c.applyAuth(req)
		if req.Header.Get("Cookie") != "sid=1" {
			t.Errorf("kino.pub request should carry Cookie, got %q", req.Header.Get("Cookie"))
		}
	})
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTruncateURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://x/v.mp4?token=secret", "https://x/v.mp4?..."},
		{"https://x/v.mp4", "https://x/v.mp4"},
		{strings.Repeat("a", 100), strings.Repeat("a", 77) + "..."},
	}
	for _, tt := range tests {
		if got := truncateURL(tt.in); got != tt.want {
			t.Errorf("truncateURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
