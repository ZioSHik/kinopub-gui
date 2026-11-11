package hlsdownloader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheKeepsSegmentsWhenOnlyCDNSessionChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "video")
	oldURL := "https://media.ams-static-01.cdntogo.net/hls/old-session/a/b/movie.mp4/seg-1-v1.ts?loc=nl"
	newURL := "https://media.ams-static-01.cdntogo.net/hls/new-session/a/b/movie.mp4/seg-1-v1.ts?loc=de"
	p := &MediaPlaylist{Segments: []Segment{{URL: oldURL, Duration: 10}}, TotalDuration: 10}
	if err := prepareTrackCache(dir, p); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "seg_00000.ts")
	if err := os.WriteFile(file, []byte("partial video"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.Segments[0].URL = newURL
	if err := prepareTrackCache(dir, p); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "partial video" {
		t.Fatalf("session refresh discarded valid segment: %q %v", data, err)
	}
	if p.Segments[0].URL != newURL {
		t.Fatal("cache identity changed the live download URL")
	}

	// A different rendition still invalidates the cache, even with the same token.
	p.Segments[0].URL = "https://media.ams-static-01.cdntogo.net/hls/new-session/a/b/movie.mp4/seg-1-a1.ts?loc=de"
	if err := prepareTrackCache(dir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("different audio/video rendition reused stale segment")
	}
}

func TestCacheDoesNotStripOtherProvidersAuthorizationPaths(t *testing.T) {
	raw := "https://other.example/hls/session/a/b/movie.mp4/seg.ts?loc=nl"
	if cacheResourceURL(raw) != raw {
		t.Fatal("unknown provider URL changed")
	}
}
