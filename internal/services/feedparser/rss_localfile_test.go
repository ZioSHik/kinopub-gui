package feedparser

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestParse_LocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.xml")
	if err := os.WriteFile(path, []byte(testRSSFeed), 0644); err != nil {
		t.Fatal(err)
	}

	// nil client: a network fetch would panic/fail, proving the local path is used.
	p := New(&http.Client{}, testLogger())
	src := domain.FeedSource{ID: "123", Token: "abc", LocalPath: path}

	series, err := p.Parse(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if series.Title != "Test Series" {
		t.Errorf("Title = %q, want %q", series.Title, "Test Series")
	}
	if len(series.Seasons) != 2 {
		t.Errorf("expected 2 seasons, got %d", len(series.Seasons))
	}
}

func TestParse_LocalFile_Missing(t *testing.T) {
	p := New(&http.Client{}, testLogger())
	src := domain.FeedSource{ID: "1", Token: "t", LocalPath: "/nonexistent/feed.xml"}

	_, err := p.Parse(context.Background(), src)
	if err == nil {
		t.Fatal("expected error for missing feed file")
	}
}
