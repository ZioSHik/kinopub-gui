package kinopubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestItemURL(t *testing.T) {
	if got := ItemURL("38290"); got != "https://kino.pub/item/view/38290" {
		t.Errorf("ItemURL = %q", got)
	}
}

func TestIsAVCCodec(t *testing.T) {
	cases := map[string]bool{
		"h264":  true,
		"H.264": true,
		"avc1":  true,
		"AVC":   true,
		"hevc":  false,
		"h265":  false,
		"":      false,
		"vp9":   false,
		"x264":  true,
		"av1":   false,
	}
	for in, want := range cases {
		if got := isAVCCodec(in); got != want {
			t.Errorf("isAVCCodec(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestGenreTitlesFlattenAndSkipEmpty(t *testing.T) {
	if got := genreTitles(nil); got != nil {
		t.Errorf("genreTitles(nil) = %v, want nil", got)
	}
	in := []NamedID{{Title: "Драма"}, {Title: ""}, {Title: "Комедия"}}
	got := genreTitles(in)
	if len(got) != 2 || got[0] != "Драма" || got[1] != "Комедия" {
		t.Errorf("genreTitles = %v, want [Драма Комедия]", got)
	}
}

// TestBestManifestAVCWinsTie: when an AVC and HEVC file share the top
// resolution, the AVC (H.264) manifest wins for browser compatibility.
func TestBestManifestAVCWinsTie(t *testing.T) {
	// HEVC listed first, AVC second — AVC must still win at equal height.
	files := []File{
		{Codec: "hevc", H: 2160, URL: FileURL{HLS4: "hevc-4k"}},
		{Codec: "h264", H: 2160, URL: FileURL{HLS4: "avc-4k"}},
	}
	if got := bestManifest(files); got != "avc-4k" {
		t.Errorf("bestManifest = %q, want avc-4k (H.264 wins tie)", got)
	}
}

func TestBestManifestAVCFirstNotOverriddenByHEVC(t *testing.T) {
	// AVC first; a later HEVC at the same height must NOT replace it.
	files := []File{
		{Codec: "h264", H: 1080, URL: FileURL{HLS4: "avc-1080"}},
		{Codec: "hevc", H: 1080, URL: FileURL{HLS4: "hevc-1080"}},
	}
	if got := bestManifest(files); got != "avc-1080" {
		t.Errorf("bestManifest = %q, want avc-1080", got)
	}
}

func TestBestManifestHigherResolutionWins(t *testing.T) {
	files := []File{
		{Codec: "h264", H: 720, URL: FileURL{HLS4: "lo"}},
		{Codec: "hevc", H: 2160, URL: FileURL{HLS4: "hi"}},
	}
	if got := bestManifest(files); got != "hi" {
		t.Errorf("bestManifest = %q, want hi (2160 > 720)", got)
	}
}

func TestBestManifestSkipsFilesWithoutManifest(t *testing.T) {
	files := []File{
		{Codec: "h264", H: 2160, URL: FileURL{HTTP: "only-http"}}, // no HLS → skipped
		{Codec: "h264", H: 1080, URL: FileURL{HLS4: "ok"}},
	}
	if got := bestManifest(files); got != "ok" {
		t.Errorf("bestManifest = %q, want ok (HTTP-only file has no manifest)", got)
	}
}

func TestBestManifestEmpty(t *testing.T) {
	if got := bestManifest(nil); got != "" {
		t.Errorf("bestManifest(nil) = %q, want empty", got)
	}
}

// TestBuildPagePlaylistMovieNumbering: when video Number is 0 it falls back to
// the 1-based index.
func TestBuildPagePlaylistMovieNumbering(t *testing.T) {
	item := Item{
		ID:    json.Number("3"),
		Type:  "movie",
		Title: "Multi",
		Videos: []Video{
			{Number: 0, Files: []File{{H: 1080, URL: FileURL{HLS4: "v1"}}}},
			{Number: 0, Files: []File{{H: 1080, URL: FileURL{HLS4: "v2"}}}},
		},
	}
	pl, err := BuildPagePlaylist(item)
	if err != nil {
		t.Fatalf("BuildPagePlaylist: %v", err)
	}
	if len(pl.Episodes) != 2 {
		t.Fatalf("got %d episodes", len(pl.Episodes))
	}
	if pl.Episodes[0].Episode != 1 || pl.Episodes[1].Episode != 2 {
		t.Errorf("episode numbering = %d,%d want 1,2", pl.Episodes[0].Episode, pl.Episodes[1].Episode)
	}
	if pl.Episodes[0].EpisodeTitle != "Multi" {
		t.Errorf("title fallback = %q, want item title", pl.Episodes[0].EpisodeTitle)
	}
}

// TestBuildPagePlaylistGenresAndType: meta is propagated.
func TestBuildPagePlaylistGenresAndType(t *testing.T) {
	item := Item{
		ID:     json.Number("8"),
		Type:   "serial",
		Title:  "T",
		Genres: []NamedID{{Title: "Драма"}, {Title: "Триллер"}},
		Seasons: []Season{{Number: 1, Episodes: []Episode{
			{Number: 1, Files: []File{{H: 1080, URL: FileURL{HLS4: "x"}}}},
		}}},
	}
	pl, err := BuildPagePlaylist(item)
	if err != nil {
		t.Fatalf("BuildPagePlaylist: %v", err)
	}
	if pl.Type != "serial" {
		t.Errorf("type = %q", pl.Type)
	}
	if len(pl.Genres) != 2 || pl.Genres[0] != "Драма" {
		t.Errorf("genres = %v", pl.Genres)
	}
}

// TestBuildPagePlaylistSerialSeasonCounts: per-season counts reflect only
// episodes that yielded a manifest.
func TestBuildPagePlaylistSerialSeasonCounts(t *testing.T) {
	item := Item{
		ID:   json.Number("11"),
		Type: "serial",
		Seasons: []Season{
			{Number: 1, Episodes: []Episode{
				{Number: 1, Files: []File{{H: 720, URL: FileURL{HLS4: "a"}}}},
				{Number: 2, Files: []File{{H: 720, URL: FileURL{HTTP: "nohls"}}}}, // no manifest → skipped
			}},
			{Number: 2, Episodes: []Episode{
				{Number: 1, Files: []File{{H: 720, URL: FileURL{HLS4: "b"}}}},
			}},
		},
	}
	pl, err := BuildPagePlaylist(item)
	if err != nil {
		t.Fatalf("BuildPagePlaylist: %v", err)
	}
	if len(pl.Episodes) != 2 {
		t.Fatalf("episodes = %d, want 2 (one skipped)", len(pl.Episodes))
	}
	counts := map[int]int{}
	for _, s := range pl.Seasons {
		counts[s.Season] = s.Count
	}
	if counts[1] != 1 || counts[2] != 1 {
		t.Errorf("season counts = %v, want {1:1, 2:1}", counts)
	}
}

// fakeLogger is a no-op domain.Logger for the scraper.
type fakeLogger struct{ infos int }

func (f *fakeLogger) Debug(string, ...domain.Field)      {}
func (f *fakeLogger) Info(string, ...domain.Field)       { f.infos++ }
func (f *fakeLogger) Warn(string, ...domain.Field)       {}
func (f *fakeLogger) Error(string, ...domain.Field)      {}
func (f *fakeLogger) With(...domain.Field) domain.Logger { return f }
func (f *fakeLogger) Component(string) domain.Logger     { return f }

func TestScraperExtractAllSeasons(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"item":{
			"id": 38290, "title": "Show", "type": "serial",
			"posters": {"big": "p.jpg"},
			"seasons": [{"number": 1, "episodes": [
				{"number": 1, "title": "Pilot", "duration": 2700, "files": [{"h": 1080, "url": {"hls4": "m1"}}]}
			]}]
		}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	log := &fakeLogger{}
	s := NewScraper(c, log)

	pl, err := s.ExtractAllSeasons(context.Background(), "https://kino.pub/item/view/38290")
	if err != nil {
		t.Fatalf("ExtractAllSeasons: %v", err)
	}
	if pl.ItemID != 38290 || pl.Title != "Show" {
		t.Errorf("playlist = %+v", pl)
	}
	if len(pl.Episodes) != 1 || pl.Episodes[0].ManifestURL != "m1" {
		t.Errorf("episodes = %+v", pl.Episodes)
	}
	if pl.Poster != "p.jpg" {
		t.Errorf("poster = %q", pl.Poster)
	}
	if log.infos == 0 {
		t.Error("logger.Info not called")
	}
}

func TestScraperExtractAllSeasonsBadURL(t *testing.T) {
	c := newTestClient(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	s := NewScraper(c, nil)
	_, err := s.ExtractAllSeasons(context.Background(), "https://kino.pub/movies")
	if err == nil || !strings.Contains(err.Error(), "cannot determine item id") {
		t.Fatalf("error = %v, want cannot determine item id", err)
	}
}

func TestScraperExtractAllSeasonsNilLoggerOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"item":{"id":5,"title":"M","type":"movie","videos":[{"number":1,"files":[{"h":1080,"url":{"hls4":"u"}}]}]}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	s := NewScraper(c, nil) // nil logger must not panic

	pl, err := s.ExtractAllSeasons(context.Background(), "5")
	if err != nil {
		t.Fatalf("ExtractAllSeasons: %v", err)
	}
	if len(pl.Episodes) != 1 {
		t.Errorf("episodes = %d", len(pl.Episodes))
	}
}

func TestScraperImplementsPageScraper(t *testing.T) {
	var _ domain.PageScraper = NewScraper(New(nil, Tokens{}), nil)
}

// keep time import used even if other helpers change
var _ = time.Second
