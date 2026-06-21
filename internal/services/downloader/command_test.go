package downloader

import (
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestToISO6392(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"ru", "rus"},
		{"en", "eng"},
		{"uk", "ukr"},
		{"de", "ger"},
		{"fr", "fre"},
		{"rus", "rus"},
		{"eng", "eng"},
		{"ukr", "ukr"},
		{"RU", "rus"},
		{"En", "eng"},
		// Unknown 2-letter code passes through lowercased.
		{"xx", "xx"},
		// Unknown 3-letter code passes through lowercased.
		{"xyz", "xyz"},
		// Longer codes pass through lowercased.
		{"abcd", "abcd"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ToISO6392(tt.input)
			if got != tt.want {
				t.Errorf("ToISO6392(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildAudioLabels(t *testing.T) {
	tests := []struct {
		name   string
		tracks []domain.AudioTrack
		want   []string
	}{
		{
			name:   "empty",
			tracks: nil,
			want:   []string{},
		},
		{
			name: "studio takes priority",
			tracks: []domain.AudioTrack{
				{Studio: "LostFilm", Language: "ru"},
				{Studio: "Кубик в Кубе", Language: "ru"},
			},
			want: []string{"LostFilm", "Кубик в Кубе"},
		},
		{
			name: "language fallback",
			tracks: []domain.AudioTrack{
				{Language: "ru"},
				{Language: "en"},
			},
			want: []string{"ru", "en"},
		},
		{
			name: "audio fallback",
			tracks: []domain.AudioTrack{
				{},
				{},
			},
			want: []string{"Audio", "Audio (2)"},
		},
		{
			name: "mixed with duplicates",
			tracks: []domain.AudioTrack{
				{Studio: "LostFilm", Language: "ru"},
				{Language: "ru"},
				{Language: "ru"},
				{},
			},
			want: []string{"LostFilm", "ru", "ru (2)", "Audio"},
		},
		{
			name: "three identical studios",
			tracks: []domain.AudioTrack{
				{Studio: "Studio"},
				{Studio: "Studio"},
				{Studio: "Studio"},
			},
			want: []string{"Studio", "Studio (2)", "Studio (3)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAudioLabels(tt.tracks)
			if len(got) != len(tt.want) {
				t.Fatalf("BuildAudioLabels() returned %d labels, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("label[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildSubtitleLabels(t *testing.T) {
	tests := []struct {
		name   string
		tracks []domain.SubtitleTrack
		want   []string
	}{
		{
			name:   "empty",
			tracks: nil,
			want:   []string{},
		},
		{
			name: "source takes priority",
			tracks: []domain.SubtitleTrack{
				{Source: "OpenSubtitles", Language: "en"},
				{Source: "Forced", Language: "ru"},
			},
			want: []string{"OpenSubtitles", "Forced"},
		},
		{
			name: "language fallback",
			tracks: []domain.SubtitleTrack{
				{Language: "ru"},
				{Language: "en"},
			},
			want: []string{"ru", "en"},
		},
		{
			name: "subtitle fallback with duplicates",
			tracks: []domain.SubtitleTrack{
				{},
				{},
			},
			want: []string{"Subtitle", "Subtitle (2)"},
		},
		{
			name: "mixed",
			tracks: []domain.SubtitleTrack{
				{Source: "SDH", Language: "en"},
				{Language: "en"},
				{},
			},
			want: []string{"SDH", "en", "Subtitle"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSubtitleLabels(tt.tracks)
			if len(got) != len(tt.want) {
				t.Fatalf("BuildSubtitleLabels() returned %d labels, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("label[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildFFmpegArgs_HLS(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaHLS,
				URL:  "https://cdn.example.com/master.m3u8",
			},
			Video: domain.VideoTrack{Index: 0, Resolution: "1920x1080"},
			Audio: []domain.AudioTrack{
				{Index: 0, Language: "ru", Studio: "LostFilm"},
				{Index: 1, Language: "en", Studio: ""},
			},
			Subtitles: []domain.SubtitleTrack{
				{Index: 0, Language: "ru", Source: "Forced"},
			},
		},
		OutPath: "/output/S01E01.mkv",
	}

	args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/S01E01.mkv.tmp", nil)

	// Verify key elements are present.
	argsStr := strings.Join(args, " ")

	// Should have -y flag.
	if !contains(args, "-y") {
		t.Error("missing -y flag")
	}

	// Should have video input.
	if !strings.Contains(argsStr, "-i https://cdn.example.com/master.m3u8") {
		t.Error("missing video input URL")
	}

	// Should map video from input 0.
	if !containsPair(args, "-map", "0:v") {
		t.Error("missing -map 0:v")
	}

	// Should map audio from inputs 1 and 2 (HLS separate inputs).
	if !containsPair(args, "-map", "1:a") {
		t.Error("missing -map 1:a for first audio")
	}
	if !containsPair(args, "-map", "2:a") {
		t.Error("missing -map 2:a for second audio")
	}

	// Should map subtitle from input 3 (after 2 audio inputs).
	if !containsPair(args, "-map", "3:s") {
		t.Error("missing -map 3:s for subtitle")
	}

	// Should have -c copy.
	if !containsPair(args, "-c", "copy") {
		t.Error("missing -c copy")
	}

	// Should have audio metadata.
	if !strings.Contains(argsStr, "title=LostFilm") {
		t.Error("missing audio title metadata for LostFilm")
	}
	if !strings.Contains(argsStr, "language=rus") {
		t.Error("missing audio language metadata for rus")
	}
	if !strings.Contains(argsStr, "title=en") {
		t.Error("missing audio title metadata for en (language fallback)")
	}
	if !strings.Contains(argsStr, "language=eng") {
		t.Error("missing audio language metadata for eng")
	}

	// Should have subtitle metadata.
	if !strings.Contains(argsStr, "title=Forced") {
		t.Error("missing subtitle title metadata")
	}
	if !strings.Contains(argsStr, "-metadata:s:s:0") {
		t.Error("missing subtitle metadata stream specifier")
	}

	// Should have progress pipe.
	if !containsPair(args, "-progress", "pipe:1") {
		t.Error("missing -progress pipe:1")
	}

	// Last arg should be the temp path.
	if args[len(args)-1] != "/tmp/S01E01.mkv.tmp" {
		t.Errorf("last arg = %q, want temp path", args[len(args)-1])
	}
}

func TestBuildFFmpegArgs_Progressive(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{
			Key: domain.EpisodeKey{Series: "test", Season: 2, Episode: 5},
		},
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
			Audio: []domain.AudioTrack{
				{Index: 0, Language: "ru", Studio: "Кубик в Кубе"},
				{Index: 1, Language: "en"},
			},
			Subtitles: nil,
		},
		OutPath: "/output/S02E05.mkv",
	}

	args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/S02E05.mkv.tmp", nil)

	// Progressive: single -i.
	inputCount := 0
	for _, a := range args {
		if a == "-i" {
			inputCount++
		}
	}
	if inputCount != 1 {
		t.Errorf("progressive should have 1 -i, got %d", inputCount)
	}

	// Audio maps should reference input 0 with stream specifiers.
	if !containsPair(args, "-map", "0:a:0") {
		t.Error("missing -map 0:a:0")
	}
	if !containsPair(args, "-map", "0:a:1") {
		t.Error("missing -map 0:a:1")
	}

	// No subtitle maps.
	for _, a := range args {
		if strings.Contains(a, ":s") && strings.HasPrefix(a, "0:s") {
			// This is fine — we just check there are no subtitle map entries.
		}
	}
	for i, a := range args {
		if a == "-map" && i+1 < len(args) && strings.Contains(args[i+1], ":s") {
			t.Error("progressive with no subtitles should not have subtitle maps")
		}
	}
}

func TestBuildFFmpegArgs_NoAudioNoSubtitles(t *testing.T) {
	job := domain.Job{
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaProgressive,
				URL:  "https://cdn.example.com/video.mp4",
			},
			Video: domain.VideoTrack{Index: 0},
		},
	}

	args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)

	// Should still have -map 0:v.
	if !containsPair(args, "-map", "0:v") {
		t.Error("missing -map 0:v")
	}

	// No audio or subtitle metadata.
	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "-metadata:s:a") {
		t.Error("should not have audio metadata with no audio tracks")
	}
	if strings.Contains(argsStr, "-metadata:s:s") {
		t.Error("should not have subtitle metadata with no subtitle tracks")
	}
}

func TestBuildFFmpegArgs_WithProxyEnv(t *testing.T) {
	job := domain.Job{
		Media: domain.ResolvedMedia{
			Source: domain.MediaSource{
				Kind: domain.MediaHLS,
				URL:  "https://cdn.example.com/master.m3u8",
			},
			Video: domain.VideoTrack{Index: 0},
		},
	}

	proxyEnv := []string{"-http_proxy http://proxy.example.com:8080"}
	args := BuildFFmpegArgs(job, proxyEnv, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "-http_proxy") {
		t.Error("missing proxy arg")
	}
	if !strings.Contains(argsStr, "http://proxy.example.com:8080") {
		t.Error("missing proxy URL")
	}
}

// Helper: check if args contains a specific value.
func contains(args []string, val string) bool {
	for _, a := range args {
		if a == val {
			return true
		}
	}
	return false
}

// Helper: check if args contains a pair (flag, value) adjacent.
func containsPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
