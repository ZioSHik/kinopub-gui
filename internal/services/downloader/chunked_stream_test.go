package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// TestStreamFrom_ServerIgnoresRangeRestartsFresh: when resuming (offset>0) but
// the server replies 200 (full file) instead of 206, streamFrom must overwrite
// from byte 0 so the file content is correct (not appended after the offset).
func TestStreamFrom_ServerIgnoresRangeRestartsFresh(t *testing.T) {
	body := []byte("HELLO-WORLD-FULL")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return the full file with 200, ignoring any Range header.
		w.Header().Set("Content-Length", itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	part := filepath.Join(dir, "v.mp4.part")
	// Pre-seed garbage partial content.
	if err := os.WriteFile(part, []byte("GARBAGEGARBAGE"), 0644); err != nil {
		t.Fatal(err)
	}

	c := newTestChunked(t, domain.RequestAuth{})
	key := domain.EpisodeKey{Season: 1, Episode: 1}
	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}

	n, err := c.streamFrom(context.Background(), srv.URL, part, int64(len("GARBAGEGARBAGE")), int64(len(body)), key, track, nil)
	if err != nil {
		t.Fatalf("streamFrom error: %v", err)
	}
	if n != int64(len(body)) {
		t.Errorf("wrote %d bytes, want %d", n, len(body))
	}
	got, _ := os.ReadFile(part)
	if string(got) != string(body) {
		t.Errorf("expected fresh overwrite %q, got %q", body, got)
	}
}

// TestStreamFrom_RangeNotSatisfiable: a 416 means we're already past the end, so
// streamFrom returns (0, nil) without error.
func TestStreamFrom_RangeNotSatisfiable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()

	dir := t.TempDir()
	part := filepath.Join(dir, "v.mp4.part")
	c := newTestChunked(t, domain.RequestAuth{})
	n, err := c.streamFrom(context.Background(), srv.URL, part, 100, 100, domain.EpisodeKey{}, domain.TrackRef{}, nil)
	if err != nil {
		t.Fatalf("expected nil error for 416, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes for 416, got %d", n)
	}
}

// TestStreamFrom_UnexpectedStatus: any other status is a hard error.
func TestStreamFrom_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	part := filepath.Join(dir, "v.mp4.part")
	c := newTestChunked(t, domain.RequestAuth{})
	_, err := c.streamFrom(context.Background(), srv.URL, part, 0, 100, domain.EpisodeKey{}, domain.TrackRef{}, nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

// TestStreamFrom_PartialContentResume: a proper 206 Partial Content response
// appends bytes after the offset.
func TestStreamFrom_PartialContentResume(t *testing.T) {
	full := []byte("0123456789ABCDEF")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Honor the Range header with a 206 serving the requested tail.
		http.ServeContent(w, r, "v.mp4", testModTime, bytesReadSeeker(full))
	}))
	defer srv.Close()

	dir := t.TempDir()
	part := filepath.Join(dir, "v.mp4.part")
	// First 6 bytes already present.
	if err := os.WriteFile(part, full[:6], 0644); err != nil {
		t.Fatal(err)
	}

	c := newTestChunked(t, domain.RequestAuth{})
	n, err := c.streamFrom(context.Background(), srv.URL, part, 6, int64(len(full)), domain.EpisodeKey{}, domain.TrackRef{}, nil)
	if err != nil {
		t.Fatalf("streamFrom error: %v", err)
	}
	if n != int64(len(full)-6) {
		t.Errorf("wrote %d bytes, want %d", n, len(full)-6)
	}
	got, _ := os.ReadFile(part)
	if string(got) != string(full) {
		t.Errorf("expected %q, got %q", full, got)
	}
}

// TestProbeSize_NoContentLength: when the server omits Content-Length, probeSize
// returns 0 (download still works, just without a percentage).
func TestProbeSize_NoContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestChunked(t, domain.RequestAuth{})
	size, err := c.probeSize(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("probeSize error: %v", err)
	}
	if size != 0 {
		t.Errorf("expected size 0 when no content length, got %d", size)
	}
}
