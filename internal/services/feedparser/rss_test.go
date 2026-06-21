package feedparser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/logx"
)

// testLogger returns a no-op logger for tests.
func testLogger() domain.Logger {
	return logx.New(nil)
}

func TestParseSeasonEpisode_TitlePrimary(t *testing.T) {
	tests := []struct {
		title    string
		link     string
		wantS    int
		wantE    int
		wantOK   bool
	}{
		{"s04e08 - Серия 8", "", 4, 8, true},
		{"S01E01 - Pilot", "", 1, 1, true},
		{"s12e99 something", "", 12, 99, true},
		{"S1E2 short", "", 1, 2, true},
		// Case insensitive
		{"S04E08", "", 4, 8, true},
		{"s04e08", "", 4, 8, true},
	}

	for _, tt := range tests {
		s, e, ok := ParseSeasonEpisode(tt.title, tt.link)
		if ok != tt.wantOK || s != tt.wantS || e != tt.wantE {
			t.Errorf("ParseSeasonEpisode(%q, %q) = (%d, %d, %v), want (%d, %d, %v)",
				tt.title, tt.link, s, e, ok, tt.wantS, tt.wantE, tt.wantOK)
		}
	}
}

func TestParseSeasonEpisode_LinkFallback(t *testing.T) {
	tests := []struct {
		title  string
		link   string
		wantS  int
		wantE  int
		wantOK bool
	}{
		{"Some title without SE", "https://kino.pub/item/view/123/s4e8/", 4, 8, true},
		{"No match", "https://kino.pub/item/view/123/S01E05/slug", 1, 5, true},
		{"No match", "https://kino.pub/item/view/123/s10e20/", 10, 20, true},
	}

	for _, tt := range tests {
		s, e, ok := ParseSeasonEpisode(tt.title, tt.link)
		if ok != tt.wantOK || s != tt.wantS || e != tt.wantE {
			t.Errorf("ParseSeasonEpisode(%q, %q) = (%d, %d, %v), want (%d, %d, %v)",
				tt.title, tt.link, s, e, ok, tt.wantS, tt.wantE, tt.wantOK)
		}
	}
}

func TestParseSeasonEpisode_NoMatch(t *testing.T) {
	_, _, ok := ParseSeasonEpisode("Random title", "https://example.com/page")
	if ok {
		t.Error("expected no match for unparseable title and link")
	}
}

func TestParseSeasonEpisode_TitleTakesPrecedence(t *testing.T) {
	// Title has s01e01, link has s02e02 — title should win.
	s, e, ok := ParseSeasonEpisode("s01e01 - Episode", "https://kino.pub/s02e02/")
	if !ok || s != 1 || e != 1 {
		t.Errorf("expected title to take precedence: got (%d, %d, %v)", s, e, ok)
	}
}

func TestClassifyEnclosure(t *testing.T) {
	tests := []struct {
		url      string
		wantKind domain.MediaKind
	}{
		{"https://cdn.example.com/video.m3u8", domain.MediaHLS},
		{"https://cdn.example.com/video.M3U8", domain.MediaHLS},
		{"https://cdn.example.com/video.mp4", domain.MediaProgressive},
		{"https://cdn.example.com/video.mkv", domain.MediaProgressive},
		{"", domain.MediaProgressive},
	}

	for _, tt := range tests {
		ms := classifyEnclosure(tt.url)
		if ms.Kind != tt.wantKind {
			t.Errorf("classifyEnclosure(%q).Kind = %v, want %v", tt.url, ms.Kind, tt.wantKind)
		}
		if ms.URL != tt.url {
			t.Errorf("classifyEnclosure(%q).URL = %q, want %q", tt.url, ms.URL, tt.url)
		}
	}
}

const testRSSFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel>
    <title>Test Series</title>
    <itunes:subtitle>Original Title</itunes:subtitle>
    <description>A test series description</description>
    <itunes:image href="https://example.com/poster.jpg"/>
    <item>
      <title>s01e01 - Pilot</title>
      <link>https://kino.pub/item/view/123/s1e1/pilot</link>
      <itunes:summary>1080p</itunes:summary>
      <enclosure url="https://cdn.example.com/s01e01.mp4"/>
    </item>
    <item>
      <title>s01e02 - Second</title>
      <link>https://kino.pub/item/view/123/s1e2/second</link>
      <itunes:summary>1080p</itunes:summary>
      <enclosure url="https://cdn.example.com/s01e02.m3u8"/>
    </item>
    <item>
      <title>s02e01 - Season Two Start</title>
      <link>https://kino.pub/item/view/123/s2e1/start</link>
      <itunes:summary>720p</itunes:summary>
      <enclosure url="https://cdn.example.com/s02e01.mp4"/>
    </item>
    <item>
      <title>Random entry without season info</title>
      <link>https://kino.pub/item/view/123/random</link>
      <itunes:summary>1080p</itunes:summary>
      <enclosure url="https://cdn.example.com/random.mp4"/>
    </item>
  </channel>
</rss>`

func TestParse_FullFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSSFeed))
	}))
	defer srv.Close()

	// Override feedURL by using a custom source that matches the test server.
	// We'll test parseRSS directly since feedURL is hardcoded.
	p := New(srv.Client(), testLogger())

	src := domain.FeedSource{ID: "123", Token: "abc"}
	series, err := p.parseRSS([]byte(testRSSFeed), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check channel-level fields.
	if series.Title != "Test Series" {
		t.Errorf("Title = %q, want %q", series.Title, "Test Series")
	}
	if series.OriginalTitle != "Original Title" {
		t.Errorf("OriginalTitle = %q, want %q", series.OriginalTitle, "Original Title")
	}
	if series.Description != "A test series description" {
		t.Errorf("Description = %q, want %q", series.Description, "A test series description")
	}
	if series.PosterURL != "https://example.com/poster.jpg" {
		t.Errorf("PosterURL = %q, want %q", series.PosterURL, "https://example.com/poster.jpg")
	}

	// Check seasons are sorted ascending.
	if len(series.Seasons) != 2 {
		t.Fatalf("expected 2 seasons, got %d", len(series.Seasons))
	}
	if series.Seasons[0].Number != 1 {
		t.Errorf("first season number = %d, want 1", series.Seasons[0].Number)
	}
	if series.Seasons[1].Number != 2 {
		t.Errorf("second season number = %d, want 2", series.Seasons[1].Number)
	}

	// Check episodes in season 1 are sorted ascending.
	s1 := series.Seasons[0]
	if len(s1.Episodes) != 2 {
		t.Fatalf("season 1: expected 2 episodes, got %d", len(s1.Episodes))
	}
	if s1.Episodes[0].Key.Episode != 1 {
		t.Errorf("s1e1 episode number = %d, want 1", s1.Episodes[0].Key.Episode)
	}
	if s1.Episodes[1].Key.Episode != 2 {
		t.Errorf("s1e2 episode number = %d, want 2", s1.Episodes[1].Key.Episode)
	}

	// Check media classification.
	if s1.Episodes[0].MediaSources[0].Kind != domain.MediaProgressive {
		t.Error("s01e01 should be progressive (mp4)")
	}
	if s1.Episodes[1].MediaSources[0].Kind != domain.MediaHLS {
		t.Error("s01e02 should be HLS (m3u8)")
	}

	// Check season 2.
	s2 := series.Seasons[1]
	if len(s2.Episodes) != 1 {
		t.Fatalf("season 2: expected 1 episode, got %d", len(s2.Episodes))
	}
	if s2.Episodes[0].Quality != "720p" {
		t.Errorf("s02e01 quality = %q, want %q", s2.Episodes[0].Quality, "720p")
	}
}

func TestParse_EmptyFeed(t *testing.T) {
	emptyFeed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Empty</title>
  </channel>
</rss>`

	p := New(&http.Client{}, testLogger())
	src := domain.FeedSource{ID: "1", Token: "t"}
	_, err := p.parseRSS([]byte(emptyFeed), src)
	if err != domain.ErrEmptyFeed {
		t.Errorf("expected ErrEmptyFeed, got %v", err)
	}
}

func TestParse_AllUnparseable(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Bad</title>
    <item>
      <title>No season info</title>
      <link>https://example.com/nope</link>
      <enclosure url="https://cdn.example.com/file.mp4"/>
    </item>
  </channel>
</rss>`

	p := New(&http.Client{}, testLogger())
	src := domain.FeedSource{ID: "1", Token: "t"}
	_, err := p.parseRSS([]byte(feed), src)
	if err != domain.ErrEmptyFeed {
		t.Errorf("expected ErrEmptyFeed, got %v", err)
	}
}

func TestParse_InvalidXML(t *testing.T) {
	p := New(&http.Client{}, testLogger())
	src := domain.FeedSource{ID: "1", Token: "t"}
	_, err := p.parseRSS([]byte("not xml at all"), src)
	if err == nil {
		t.Error("expected error for invalid XML")
	}
}

func TestParse_HTTPRetrieval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the URL path matches expected format.
		if r.URL.Path != "/podcast/get/42/mytoken" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSSFeed))
	}))
	defer srv.Close()

	// We need to override the feed URL for testing. Since feedURL is hardcoded
	// to kino.pub, we test the full Parse flow by using a custom transport.
	transport := &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL,
	}
	client := &http.Client{Transport: transport}

	p := New(client, testLogger())
	src := domain.FeedSource{ID: "42", Token: "mytoken"}

	series, err := p.Parse(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if series.Title != "Test Series" {
		t.Errorf("Title = %q, want %q", series.Title, "Test Series")
	}
}

func TestParse_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	transport := &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL,
	}
	client := &http.Client{Transport: transport}

	p := New(client, testLogger())
	src := domain.FeedSource{ID: "1", Token: "t"}

	_, err := p.Parse(context.Background(), src)
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

// rewriteTransport redirects all requests to a test server URL.
type rewriteTransport struct {
	base    http.RoundTripper
	rewrite string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Preserve the path but redirect to the test server.
	newURL := t.rewrite + req.URL.Path
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	return t.base.RoundTrip(newReq)
}
