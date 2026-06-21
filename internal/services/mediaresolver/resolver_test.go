package mediaresolver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// --- SelectMediaSource tests ---

func TestSelectMediaSource_ExactMatch(t *testing.T) {
	sources := []domain.MediaSource{
		{Kind: domain.MediaHLS, URL: "http://a/720.m3u8", Quality: "720p"},
		{Kind: domain.MediaHLS, URL: "http://a/1080.m3u8", Quality: "1080p"},
		{Kind: domain.MediaHLS, URL: "http://a/480.m3u8", Quality: "480p"},
	}

	got := SelectMediaSource(sources, "1080p")
	if got.Quality != "1080p" {
		t.Errorf("expected 1080p, got %s", got.Quality)
	}
}

func TestSelectMediaSource_NoMatchPicksHighest(t *testing.T) {
	sources := []domain.MediaSource{
		{Kind: domain.MediaHLS, URL: "http://a/720.m3u8", Quality: "720p"},
		{Kind: domain.MediaHLS, URL: "http://a/1080.m3u8", Quality: "1080p"},
		{Kind: domain.MediaHLS, URL: "http://a/480.m3u8", Quality: "480p"},
	}

	got := SelectMediaSource(sources, "4k")
	if got.Quality != "1080p" {
		t.Errorf("expected 1080p (highest), got %s", got.Quality)
	}
}

func TestSelectMediaSource_NoPrefPicksHighest(t *testing.T) {
	sources := []domain.MediaSource{
		{Kind: domain.MediaHLS, URL: "http://a/480.m3u8", Quality: "480p"},
		{Kind: domain.MediaHLS, URL: "http://a/1080.m3u8", Quality: "1080p"},
		{Kind: domain.MediaHLS, URL: "http://a/720.m3u8", Quality: "720p"},
	}

	got := SelectMediaSource(sources, "")
	if got.Quality != "1080p" {
		t.Errorf("expected 1080p (highest), got %s", got.Quality)
	}
}

func TestSelectMediaSource_EmptySources(t *testing.T) {
	got := SelectMediaSource(nil, "1080p")
	if got.URL != "" {
		t.Errorf("expected empty source, got %+v", got)
	}
}

// --- HLS parsing tests ---

func TestParseM3U8_BasicPlaylist(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080
video_1080.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720
video_720.m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Studio One",LANGUAGE="rus",URI="audio_rus.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Original",LANGUAGE="eng",URI="audio_eng.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Russian",LANGUAGE="rus",URI="subs_rus.m3u8"
`
	source := domain.MediaSource{Kind: domain.MediaHLS, URL: "http://example.com/master.m3u8", Quality: "1080p"}
	resolved, err := parseM3U8(strings.NewReader(playlist), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should pick highest bandwidth variant.
	if resolved.Video.Bandwidth != 5000000 {
		t.Errorf("expected bandwidth 5000000, got %d", resolved.Video.Bandwidth)
	}
	if resolved.Video.Resolution != "1920x1080" {
		t.Errorf("expected resolution 1920x1080, got %s", resolved.Video.Resolution)
	}

	if len(resolved.Audio) != 2 {
		t.Fatalf("expected 2 audio tracks, got %d", len(resolved.Audio))
	}
	if resolved.Audio[0].Studio != "Studio One" {
		t.Errorf("expected studio 'Studio One', got %q", resolved.Audio[0].Studio)
	}
	if resolved.Audio[0].Language != "rus" {
		t.Errorf("expected language 'rus', got %q", resolved.Audio[0].Language)
	}
	if resolved.Audio[1].Studio != "Original" {
		t.Errorf("expected studio 'Original', got %q", resolved.Audio[1].Studio)
	}

	if len(resolved.Subtitles) != 1 {
		t.Fatalf("expected 1 subtitle track, got %d", len(resolved.Subtitles))
	}
	if resolved.Subtitles[0].Source != "Russian" {
		t.Errorf("expected subtitle source 'Russian', got %q", resolved.Subtitles[0].Source)
	}
	if resolved.Subtitles[0].Language != "rus" {
		t.Errorf("expected subtitle language 'rus', got %q", resolved.Subtitles[0].Language)
	}
}

func TestParseM3U8_NoVideo(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Studio",LANGUAGE="rus",URI="audio.m3u8"
`
	source := domain.MediaSource{Kind: domain.MediaHLS, URL: "http://example.com/master.m3u8"}
	_, err := parseM3U8(strings.NewReader(playlist), source)
	if err != domain.ErrNoVideoTrack {
		t.Errorf("expected ErrNoVideoTrack, got %v", err)
	}
}

// --- ffprobe parsing tests ---

func TestParseFFprobeOutput_Basic(t *testing.T) {
	jsonData := `{
		"streams": [
			{
				"index": 0,
				"codec_type": "video",
				"width": 1920,
				"height": 1080,
				"bit_rate": "5000000",
				"tags": {}
			},
			{
				"index": 1,
				"codec_type": "audio",
				"tags": {"language": "rus", "title": "Studio Dubbing"}
			},
			{
				"index": 2,
				"codec_type": "audio",
				"tags": {"language": "eng", "title": "Original"}
			}
		]
	}`

	source := domain.MediaSource{Kind: domain.MediaProgressive, URL: "http://example.com/video.mp4", Quality: "1080p"}
	resolved, err := parseFFprobeOutput([]byte(jsonData), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Video.Resolution != "1920x1080" {
		t.Errorf("expected resolution 1920x1080, got %s", resolved.Video.Resolution)
	}
	if resolved.Video.Index != 0 {
		t.Errorf("expected video index 0, got %d", resolved.Video.Index)
	}

	if len(resolved.Audio) != 2 {
		t.Fatalf("expected 2 audio tracks, got %d", len(resolved.Audio))
	}
	if resolved.Audio[0].Language != "rus" {
		t.Errorf("expected language 'rus', got %q", resolved.Audio[0].Language)
	}
	if resolved.Audio[0].Studio != "Studio Dubbing" {
		t.Errorf("expected studio 'Studio Dubbing', got %q", resolved.Audio[0].Studio)
	}

	if resolved.Subtitles != nil {
		t.Errorf("expected nil subtitles for progressive, got %v", resolved.Subtitles)
	}
}

func TestParseFFprobeOutput_NoVideo(t *testing.T) {
	jsonData := `{
		"streams": [
			{
				"index": 0,
				"codec_type": "audio",
				"tags": {"language": "eng"}
			}
		]
	}`

	source := domain.MediaSource{Kind: domain.MediaProgressive, URL: "http://example.com/audio.mp4"}
	_, err := parseFFprobeOutput([]byte(jsonData), source)
	if err != domain.ErrNoVideoTrack {
		t.Errorf("expected ErrNoVideoTrack, got %v", err)
	}
}

func TestParseFFprobeOutput_InvalidJSON(t *testing.T) {
	source := domain.MediaSource{Kind: domain.MediaProgressive, URL: "http://example.com/video.mp4"}
	_, err := parseFFprobeOutput([]byte("not json"), source)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// --- Integration-style tests with Resolver ---

type noopLogger struct{}

func (l noopLogger) Debug(_ string, _ ...domain.Field) {}
func (l noopLogger) Info(_ string, _ ...domain.Field)  {}
func (l noopLogger) Warn(_ string, _ ...domain.Field)  {}
func (l noopLogger) Error(_ string, _ ...domain.Field) {}
func (l noopLogger) With(_ ...domain.Field) domain.Logger {
	return l
}
func (l noopLogger) Component(_ string) domain.Logger {
	return l
}

func TestResolver_HLS(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080
video.m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Dubbing",LANGUAGE="rus",URI="audio.m3u8"
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, playlist)
	}))
	defer srv.Close()

	resolver := New(srv.Client(), nil, noopLogger{})

	ep := domain.Episode{
		Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		MediaSources: []domain.MediaSource{
			{Kind: domain.MediaHLS, URL: srv.URL + "/master.m3u8", Quality: "1080p"},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), ep, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Video.Resolution != "1920x1080" {
		t.Errorf("expected 1920x1080, got %s", resolved.Video.Resolution)
	}
	if len(resolved.Audio) != 1 {
		t.Fatalf("expected 1 audio track, got %d", len(resolved.Audio))
	}
	if resolved.Audio[0].Studio != "Dubbing" {
		t.Errorf("expected studio 'Dubbing', got %q", resolved.Audio[0].Studio)
	}
}

func TestResolver_Progressive(t *testing.T) {
	ffprobeJSON := `{
		"streams": [
			{"index": 0, "codec_type": "video", "width": 1280, "height": 720, "tags": {}},
			{"index": 1, "codec_type": "audio", "tags": {"language": "eng", "title": "English"}}
		]
	}`

	runOutput := func(_ context.Context, _ string, _ []string, _ []string) ([]byte, error) {
		return []byte(ffprobeJSON), nil
	}

	resolver := New(http.DefaultClient, runOutput, noopLogger{})

	ep := domain.Episode{
		Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		MediaSources: []domain.MediaSource{
			{Kind: domain.MediaProgressive, URL: "http://cdn.example.com/video.mp4", Quality: "720p"},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), ep, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Video.Resolution != "1280x720" {
		t.Errorf("expected 1280x720, got %s", resolved.Video.Resolution)
	}
	if len(resolved.Audio) != 1 {
		t.Fatalf("expected 1 audio track, got %d", len(resolved.Audio))
	}
	if resolved.Audio[0].Language != "eng" {
		t.Errorf("expected language 'eng', got %q", resolved.Audio[0].Language)
	}
}

func TestResolver_NoMediaSources(t *testing.T) {
	resolver := New(http.DefaultClient, nil, noopLogger{})

	ep := domain.Episode{
		Key:          domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		MediaSources: nil,
	}

	_, err := resolver.Resolve(context.Background(), ep, "")
	if err != domain.ErrNoVideoTrack {
		t.Errorf("expected ErrNoVideoTrack, got %v", err)
	}
}

func TestResolver_Timeout(t *testing.T) {
	// Use an already-cancelled context to simulate timeout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never reach here.
		fmt.Fprint(w, "#EXTM3U\n")
	}))
	defer srv.Close()

	resolver := New(srv.Client(), nil, noopLogger{})

	ep := domain.Episode{
		Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		MediaSources: []domain.MediaSource{
			{Kind: domain.MediaHLS, URL: srv.URL + "/master.m3u8", Quality: "1080p"},
		},
	}

	_, err := resolver.Resolve(ctx, ep, "")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// --- qualityScore tests ---

func TestQualityScore(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"4k", 2160},
		{"2160p", 2160},
		{"1080p", 1080},
		{"720p", 720},
		{"480p", 480},
		{"360p", 360},
		{"240p", 240},
		{"1440p", 1440},
		{"unknown", 0},
		{"", 0},
	}

	for _, tt := range tests {
		got := qualityScore(tt.input)
		if got != tt.want {
			t.Errorf("qualityScore(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
