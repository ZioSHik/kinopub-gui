package downloader

import (
	"strings"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// indexOf returns the index of the first occurrence of val in args, or -1.
func indexOf(args []string, val string) int {
	for i, a := range args {
		if a == val {
			return i
		}
	}
	return -1
}

func TestBuildRemuxArgs_BasicMKV(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{
			Key:   domain.EpisodeKey{Series: "s", Season: 3, Episode: 7},
			Title: "The Episode",
		},
		SeriesTitle: "My Show",
		OutPath:     "/out/S03E07.mkv",
	}

	args := BuildRemuxArgs(job, "/tmp/local.ts", "/tmp/S03E07.mkv.tmp")

	// Must start with -y.
	if args[0] != "-y" {
		t.Errorf("expected first arg -y, got %q", args[0])
	}

	// Single local input, no auth.
	if !containsPair(args, "-i", "/tmp/local.ts") {
		t.Error("missing -i local input")
	}
	if indexOf(args, "-user_agent") != -1 || indexOf(args, "-headers") != -1 {
		t.Error("remux must not inject auth options for local files")
	}

	// -map 0 to copy all streams.
	if !containsPair(args, "-map", "0") {
		t.Error("missing -map 0")
	}

	// -c copy.
	if !containsPair(args, "-c", "copy") {
		t.Error("missing -c copy")
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "title=The Episode") {
		t.Error("missing episode title metadata")
	}
	if !strings.Contains(argsStr, "SHOW=My Show") {
		t.Error("missing SHOW metadata")
	}
	if !containsPair(args, "-metadata", "episode_sort=7") {
		t.Error("missing episode_sort=7")
	}
	if !containsPair(args, "-metadata", "season_number=3") {
		t.Error("missing season_number=3")
	}
	if !containsPair(args, "-metadata", "episode_id=S03E07") {
		t.Error("missing episode_id=S03E07")
	}

	// matroska format and output path last.
	if !containsPair(args, "-f", "matroska") {
		t.Error("expected -f matroska for .mkv output")
	}
	if args[len(args)-1] != "/tmp/S03E07.mkv.tmp" {
		t.Errorf("last arg = %q, want temp path", args[len(args)-1])
	}
}

func TestBuildRemuxArgs_MP4Format(t *testing.T) {
	job := domain.Job{OutPath: "/out/x.mp4"}
	args := BuildRemuxArgs(job, "/tmp/in.ts", "/tmp/x.mp4.tmp")
	if !containsPair(args, "-f", "mp4") {
		t.Error("expected -f mp4 for .mp4 temp path")
	}
}

func TestBuildRemuxArgs_PosterOnlyForMKV(t *testing.T) {
	t.Run("mkv attaches poster", func(t *testing.T) {
		job := domain.Job{OutPath: "/out/x.mkv", PosterPath: "/tmp/poster.jpg"}
		args := BuildRemuxArgs(job, "/tmp/in.ts", "/tmp/x.mkv.tmp")
		if !containsPair(args, "-attach", "/tmp/poster.jpg") {
			t.Error("expected -attach poster for mkv")
		}
		if !containsPair(args, "-metadata:s:t:0", "mimetype=image/jpeg") {
			t.Error("expected poster mimetype metadata")
		}
		if !containsPair(args, "-metadata:s:t:0", "filename=cover.jpg") {
			t.Error("expected poster filename metadata")
		}
	})
	t.Run("mp4 does not attach poster", func(t *testing.T) {
		job := domain.Job{OutPath: "/out/x.mp4", PosterPath: "/tmp/poster.jpg"}
		args := BuildRemuxArgs(job, "/tmp/in.ts", "/tmp/x.mp4.tmp")
		if indexOf(args, "-attach") != -1 {
			t.Error("mp4 must not attach poster (matroska-only feature)")
		}
	})
}

func TestBuildRemuxArgs_NoOptionalMetadata(t *testing.T) {
	// Empty episode title and series title → those -metadata entries absent,
	// but season/episode numeric metadata always present.
	job := domain.Job{
		Episode: domain.Episode{Key: domain.EpisodeKey{Season: 0, Episode: 0}},
		OutPath: "/out/x.mkv",
	}
	args := BuildRemuxArgs(job, "/tmp/in.ts", "/tmp/x.mkv.tmp")
	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "title=") {
		t.Error("should not emit episode title when empty")
	}
	if strings.Contains(argsStr, "SHOW=") {
		t.Error("should not emit SHOW when empty")
	}
	if !containsPair(args, "-metadata", "episode_id=S00E00") {
		t.Error("expected episode_id=S00E00")
	}
}

func TestBuildHLSMuxArgs_WithSeparateAudio(t *testing.T) {
	job := domain.Job{
		Episode: domain.Episode{
			Key:   domain.EpisodeKey{Season: 1, Episode: 2},
			Title: "Ep",
		},
		OutPath: "/out/o.mkv",
	}
	hls := &domain.HLSDownloadResult{
		VideoPath: "/tmp/video.ts",
		AudioTracks: []domain.HLSAudioTrack{
			{Path: "/tmp/a0.ts", Name: "LostFilm", Language: "ru"},
			{Path: "/tmp/a1.ts", Name: "", Language: "en"},
		},
	}

	args := BuildHLSMuxArgs(job, hls, "/tmp/o.mkv.tmp")

	if args[0] != "-y" {
		t.Errorf("expected -y first, got %q", args[0])
	}

	// Inputs: video first, then audio in order.
	iVideo := indexOf(args, "/tmp/video.ts")
	iA0 := indexOf(args, "/tmp/a0.ts")
	iA1 := indexOf(args, "/tmp/a1.ts")
	if !(iVideo >= 0 && iA0 > iVideo && iA1 > iA0) {
		t.Errorf("inputs out of order: video=%d a0=%d a1=%d", iVideo, iA0, iA1)
	}

	// Maps.
	if !containsPair(args, "-map", "0:v:0") {
		t.Error("missing -map 0:v:0")
	}
	if !containsPair(args, "-map", "1:a:0") {
		t.Error("missing -map 1:a:0")
	}
	if !containsPair(args, "-map", "2:a:0") {
		t.Error("missing -map 2:a:0")
	}
	// Must NOT have the no-audio fallback when separate audio present.
	if containsPair(args, "-map", "0:a?") {
		t.Error("should not use 0:a? fallback when separate audio tracks exist")
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "title=LostFilm") {
		t.Error("missing LostFilm title")
	}
	if !strings.Contains(argsStr, "language=rus") {
		t.Error("missing language=rus")
	}
	// Second track name empty → falls back to language label "en".
	if !strings.Contains(argsStr, "title=en") {
		t.Error("missing fallback title=en")
	}
	if !strings.Contains(argsStr, "language=eng") {
		t.Error("missing language=eng")
	}
}

func TestBuildHLSMuxArgs_NoSeparateAudioUsesFallbackMap(t *testing.T) {
	job := domain.Job{OutPath: "/out/o.mkv"}
	hls := &domain.HLSDownloadResult{VideoPath: "/tmp/video.ts"}

	args := BuildHLSMuxArgs(job, hls, "/tmp/o.mkv.tmp")

	if !containsPair(args, "-map", "0:v:0") {
		t.Error("missing -map 0:v:0")
	}
	if !containsPair(args, "-map", "0:a?") {
		t.Error("expected -map 0:a? fallback when no separate audio")
	}
	// Only one input.
	inputs := 0
	for _, a := range args {
		if a == "-i" {
			inputs++
		}
	}
	if inputs != 1 {
		t.Errorf("expected 1 input, got %d", inputs)
	}
}

func TestBuildHLSMuxArgs_DuplicateAudioLabelsUnique(t *testing.T) {
	job := domain.Job{OutPath: "/out/o.mkv"}
	hls := &domain.HLSDownloadResult{
		VideoPath: "/tmp/v.ts",
		AudioTracks: []domain.HLSAudioTrack{
			{Path: "/tmp/a0.ts", Name: "Dub"},
			{Path: "/tmp/a1.ts", Name: "Dub"},
		},
	}
	args := BuildHLSMuxArgs(job, hls, "/tmp/o.mkv.tmp")
	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "title=Dub") {
		t.Error("missing first Dub title")
	}
	if !strings.Contains(argsStr, "title=Dub (2)") {
		t.Error("expected deduped 'Dub (2)' for second identical label")
	}
}

func TestBuildHLSMuxArgs_AudioFallbackLabel(t *testing.T) {
	job := domain.Job{OutPath: "/out/o.mkv"}
	hls := &domain.HLSDownloadResult{
		VideoPath:   "/tmp/v.ts",
		AudioTracks: []domain.HLSAudioTrack{{Path: "/tmp/a0.ts"}}, // no name, no language
	}
	args := BuildHLSMuxArgs(job, hls, "/tmp/o.mkv.tmp")
	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "title=Audio") {
		t.Error("expected 'Audio' fallback label")
	}
	// No language metadata since Language empty.
	if strings.Contains(argsStr, "language=") {
		t.Error("should not emit language for track without language")
	}
}

func TestBuildHLSMuxArgs_MP4Format(t *testing.T) {
	job := domain.Job{OutPath: "/out/o.mp4"}
	hls := &domain.HLSDownloadResult{VideoPath: "/tmp/v.ts"}
	args := BuildHLSMuxArgs(job, hls, "/tmp/o.mp4.tmp")
	if !containsPair(args, "-f", "mp4") {
		t.Error("expected -f mp4")
	}
	if args[len(args)-1] != "/tmp/o.mp4.tmp" {
		t.Errorf("last arg = %q, want temp path", args[len(args)-1])
	}
}
