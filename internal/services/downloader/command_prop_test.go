package downloader

import (
	"fmt"
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// **Validates: Requirements 7.1, 7.2, 8.1, 8.2, 8.3, 8.6, 8.7, 9.1, 9.2, 9.3, 9.4, 9.7**

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// genLanguage2 generates a known 2-letter language code from the iso639Map.
func genLanguage2() *rapid.Generator[string] {
	codes := make([]string, 0, len(iso639Map))
	for k := range iso639Map {
		codes = append(codes, k)
	}
	return rapid.SampledFrom(codes)
}

// genOptionalString generates either an empty string or a non-empty safe string.
func genOptionalString() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.Just(""),
		rapid.StringMatching(`[A-Za-zА-Яа-я0-9 _\-]{1,30}`),
	)
}

// genNonEmptyString generates a non-empty safe string.
func genNonEmptyString() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-zА-Яа-я0-9 _\-]{1,30}`)
}

// genAudioTrack generates a random AudioTrack.
func genAudioTrack() *rapid.Generator[domain.AudioTrack] {
	return rapid.Custom(func(t *rapid.T) domain.AudioTrack {
		return domain.AudioTrack{
			Index:    rapid.IntRange(0, 10).Draw(t, "index"),
			GroupID:  genOptionalString().Draw(t, "groupID"),
			Language: genOptionalString().Draw(t, "language"),
			Studio:   genOptionalString().Draw(t, "studio"),
		}
	})
}

// genSubtitleTrack generates a random SubtitleTrack.
func genSubtitleTrack() *rapid.Generator[domain.SubtitleTrack] {
	return rapid.Custom(func(t *rapid.T) domain.SubtitleTrack {
		return domain.SubtitleTrack{
			Index:    rapid.IntRange(0, 10).Draw(t, "index"),
			GroupID:  genOptionalString().Draw(t, "groupID"),
			Language: genOptionalString().Draw(t, "language"),
			Source:   genOptionalString().Draw(t, "source"),
		}
	})
}

// genMediaKind generates either HLS or Progressive.
func genMediaKind() *rapid.Generator[domain.MediaKind] {
	return rapid.SampledFrom([]domain.MediaKind{domain.MediaHLS, domain.MediaProgressive})
}

// genJob generates a Job with a random number of audio and subtitle tracks.
func genJob(numAudio, numSubs int) *rapid.Generator[domain.Job] {
	return rapid.Custom(func(t *rapid.T) domain.Job {
		kind := genMediaKind().Draw(t, "mediaKind")
		url := fmt.Sprintf("https://cdn.example.com/%s",
			rapid.StringMatching(`[a-z]{3,10}\.(m3u8|mp4)`).Draw(t, "url"))

		audio := make([]domain.AudioTrack, numAudio)
		for i := range audio {
			audio[i] = genAudioTrack().Draw(t, fmt.Sprintf("audio_%d", i))
			audio[i].Index = i
		}

		subs := make([]domain.SubtitleTrack, numSubs)
		for i := range subs {
			subs[i] = genSubtitleTrack().Draw(t, fmt.Sprintf("sub_%d", i))
			subs[i].Index = i
		}

		return domain.Job{
			Episode: domain.Episode{
				Key: domain.EpisodeKey{
					Series:  domain.SeriesID("test-series"),
					Season:  rapid.IntRange(1, 20).Draw(t, "season"),
					Episode: rapid.IntRange(1, 50).Draw(t, "episode"),
				},
			},
			Media: domain.ResolvedMedia{
				Source: domain.MediaSource{
					Kind: kind,
					URL:  url,
				},
				Video: domain.VideoTrack{Index: 0, Resolution: "1920x1080"},
				Audio: audio,
				Subtitles: subs,
			},
			OutPath: "/output/S01E01.mkv",
		}
	})
}

// ---------------------------------------------------------------------------
// Property 20: ffmpeg command maps every track to a temp output
// ---------------------------------------------------------------------------

// For any Job with N audio tracks and M subtitle tracks, BuildFFmpegArgs
// produces args that contain a -map for the video, N audio maps, M subtitle
// maps, and the last arg is the temp path.
func TestProperty20_FFmpegCommandMapsEveryTrack(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numAudio := rapid.IntRange(0, 8).Draw(t, "numAudio")
		numSubs := rapid.IntRange(0, 5).Draw(t, "numSubs")
		job := genJob(numAudio, numSubs).Draw(t, "job")
		tempPath := "/tmp/episode.mkv.tmp"

		args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, tempPath, nil)

		// Count -map entries.
		var videoMaps, audioMaps, subMaps int
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-map" {
				val := args[i+1]
				switch {
				case strings.Contains(val, ":v"):
					videoMaps++
				case strings.Contains(val, ":a"):
					audioMaps++
				case strings.Contains(val, ":s"):
					subMaps++
				}
			}
		}

		if videoMaps != 1 {
			t.Fatalf("expected 1 video -map, got %d", videoMaps)
		}
		if audioMaps != numAudio {
			t.Fatalf("expected %d audio -map entries, got %d", numAudio, audioMaps)
		}
		if subMaps != numSubs {
			t.Fatalf("expected %d subtitle -map entries, got %d", numSubs, subMaps)
		}

		// Verify -c copy is present.
		foundCopy := false
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-c" && args[i+1] == "copy" {
				foundCopy = true
				break
			}
		}
		if !foundCopy {
			t.Fatal("missing -c copy in args")
		}

		// Last arg must be the temp path.
		if args[len(args)-1] != tempPath {
			t.Fatalf("last arg = %q, want %q", args[len(args)-1], tempPath)
		}

		// Temp path must differ from the job's final output path.
		if tempPath == job.OutPath {
			t.Fatal("temp path must differ from final output path")
		}
	})
}

// ---------------------------------------------------------------------------
// Property 21: Audio labels are informative, deterministic, and unique
// ---------------------------------------------------------------------------

// For any list of AudioTracks, BuildAudioLabels returns exactly len(tracks)
// labels, all non-empty, all unique, and deterministic (same input → same output).
func TestProperty21_AudioLabelsInformativeDeterministicUnique(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numTracks := rapid.IntRange(0, 15).Draw(t, "numTracks")
		tracks := make([]domain.AudioTrack, numTracks)
		for i := range tracks {
			tracks[i] = genAudioTrack().Draw(t, fmt.Sprintf("track_%d", i))
		}

		labels := BuildAudioLabels(tracks)

		// Exactly len(tracks) labels.
		if len(labels) != len(tracks) {
			t.Fatalf("expected %d labels, got %d", len(tracks), len(labels))
		}

		// All non-empty.
		for i, l := range labels {
			if l == "" {
				t.Fatalf("label[%d] is empty", i)
			}
		}

		// All unique.
		seen := make(map[string]bool)
		for i, l := range labels {
			if seen[l] {
				t.Fatalf("duplicate label at index %d: %q", i, l)
			}
			seen[l] = true
		}

		// Deterministic: calling again with same input produces same output.
		labels2 := BuildAudioLabels(tracks)
		for i := range labels {
			if labels[i] != labels2[i] {
				t.Fatalf("non-deterministic: label[%d] = %q then %q", i, labels[i], labels2[i])
			}
		}

		// Informative: studio is used when present, else language, else fallback.
		for i, track := range tracks {
			switch {
			case track.Studio != "":
				if !strings.Contains(labels[i], track.Studio) {
					t.Fatalf("label[%d] = %q does not contain studio %q", i, labels[i], track.Studio)
				}
			case track.Language != "":
				if !strings.Contains(labels[i], track.Language) {
					t.Fatalf("label[%d] = %q does not contain language %q", i, labels[i], track.Language)
				}
			default:
				if !strings.Contains(labels[i], "Audio") {
					t.Fatalf("label[%d] = %q does not contain fallback 'Audio'", i, labels[i])
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 22: Audio language maps to ISO 639-2
// ---------------------------------------------------------------------------

// For any AudioTrack with a known 2-letter language code, the metadata in
// BuildFFmpegArgs contains the corresponding ISO 639-2 (3-letter) code.
func TestProperty22_AudioLanguageMapsToISO6392(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lang2 := genLanguage2().Draw(t, "lang2")
		expected3 := iso639Map[lang2]

		job := domain.Job{
			Episode: domain.Episode{
				Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
			},
			Media: domain.ResolvedMedia{
				Source: domain.MediaSource{
					Kind: domain.MediaProgressive,
					URL:  "https://cdn.example.com/video.mp4",
				},
				Video: domain.VideoTrack{Index: 0},
				Audio: []domain.AudioTrack{
					{Index: 0, Language: lang2, Studio: "TestStudio"},
				},
			},
		}

		args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)
		argsStr := strings.Join(args, " ")

		// The args should contain language=<3-letter-code>.
		expectedMeta := fmt.Sprintf("language=%s", expected3)
		if !strings.Contains(argsStr, expectedMeta) {
			t.Fatalf("args do not contain %q for input lang %q; args: %s",
				expectedMeta, lang2, argsStr)
		}

		// Also verify via ToISO6392 directly.
		got := ToISO6392(lang2)
		if got != expected3 {
			t.Fatalf("ToISO6392(%q) = %q, want %q", lang2, got, expected3)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 23: Subtitle labels are informative, deterministic, and unique
// ---------------------------------------------------------------------------

// For any list of SubtitleTracks, BuildSubtitleLabels returns exactly len(tracks)
// labels, all non-empty, all unique, and deterministic.
func TestProperty23_SubtitleLabelsInformativeDeterministicUnique(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numTracks := rapid.IntRange(0, 15).Draw(t, "numTracks")
		tracks := make([]domain.SubtitleTrack, numTracks)
		for i := range tracks {
			tracks[i] = genSubtitleTrack().Draw(t, fmt.Sprintf("track_%d", i))
		}

		labels := BuildSubtitleLabels(tracks)

		// Exactly len(tracks) labels.
		if len(labels) != len(tracks) {
			t.Fatalf("expected %d labels, got %d", len(tracks), len(labels))
		}

		// All non-empty.
		for i, l := range labels {
			if l == "" {
				t.Fatalf("label[%d] is empty", i)
			}
		}

		// All unique.
		seen := make(map[string]bool)
		for i, l := range labels {
			if seen[l] {
				t.Fatalf("duplicate label at index %d: %q", i, l)
			}
			seen[l] = true
		}

		// Deterministic: calling again with same input produces same output.
		labels2 := BuildSubtitleLabels(tracks)
		for i := range labels {
			if labels[i] != labels2[i] {
				t.Fatalf("non-deterministic: label[%d] = %q then %q", i, labels[i], labels2[i])
			}
		}

		// Informative: source is used when present, else language, else fallback.
		for i, track := range tracks {
			switch {
			case track.Source != "":
				if !strings.Contains(labels[i], track.Source) {
					t.Fatalf("label[%d] = %q does not contain source %q", i, labels[i], track.Source)
				}
			case track.Language != "":
				if !strings.Contains(labels[i], track.Language) {
					t.Fatalf("label[%d] = %q does not contain language %q", i, labels[i], track.Language)
				}
			default:
				if !strings.Contains(labels[i], "Subtitle") {
					t.Fatalf("label[%d] = %q does not contain fallback 'Subtitle'", i, labels[i])
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 24: Track preservation — all tracks are mapped regardless of attributes
// ---------------------------------------------------------------------------

// For the command builder, verify that BuildFFmpegArgs includes all tracks
// regardless of their attributes (even empty studio/language).
func TestProperty24_TrackPreservationRegardlessOfAttributes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numAudio := rapid.IntRange(1, 8).Draw(t, "numAudio")
		numSubs := rapid.IntRange(0, 5).Draw(t, "numSubs")

		audio := make([]domain.AudioTrack, numAudio)
		for i := range audio {
			// Some tracks may have empty studio and language.
			audio[i] = domain.AudioTrack{
				Index:    i,
				Studio:   genOptionalString().Draw(t, fmt.Sprintf("studio_%d", i)),
				Language: genOptionalString().Draw(t, fmt.Sprintf("lang_%d", i)),
			}
		}

		subs := make([]domain.SubtitleTrack, numSubs)
		for i := range subs {
			subs[i] = domain.SubtitleTrack{
				Index:    i,
				Source:   genOptionalString().Draw(t, fmt.Sprintf("source_%d", i)),
				Language: genOptionalString().Draw(t, fmt.Sprintf("sublang_%d", i)),
			}
		}

		job := domain.Job{
			Episode: domain.Episode{
				Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
			},
			Media: domain.ResolvedMedia{
				Source: domain.MediaSource{
					Kind: genMediaKind().Draw(t, "kind"),
					URL:  "https://cdn.example.com/video.m3u8",
				},
				Video:     domain.VideoTrack{Index: 0},
				Audio:     audio,
				Subtitles: subs,
			},
		}

		args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)

		// Count audio and subtitle -map entries.
		var audioMaps, subMaps int
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-map" {
				val := args[i+1]
				if strings.Contains(val, ":a") {
					audioMaps++
				} else if strings.Contains(val, ":s") {
					subMaps++
				}
			}
		}

		// Every audio track must be mapped, even those with empty attributes.
		if audioMaps != numAudio {
			t.Fatalf("expected %d audio maps (all tracks preserved), got %d", numAudio, audioMaps)
		}

		// Every subtitle track must be mapped.
		if subMaps != numSubs {
			t.Fatalf("expected %d subtitle maps (all tracks preserved), got %d", numSubs, subMaps)
		}

		// Every audio track gets a title metadata entry.
		audioMetaCount := 0
		for _, a := range args {
			if strings.HasPrefix(a, "-metadata:s:a:") {
				audioMetaCount++
			}
		}
		// Each audio track gets at least a title entry (and optionally a language entry).
		if audioMetaCount < numAudio {
			t.Fatalf("expected at least %d audio metadata entries (one title per track), got %d",
				numAudio, audioMetaCount)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 25: Absent subtitles produce no track, no warning, no error
// ---------------------------------------------------------------------------

// For any Job with zero SubtitleTracks, BuildFFmpegArgs produces no subtitle
// -map entries and no -metadata:s:s entries.
func TestProperty25_AbsentSubtitlesProduceNoTrack(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numAudio := rapid.IntRange(0, 8).Draw(t, "numAudio")

		audio := make([]domain.AudioTrack, numAudio)
		for i := range audio {
			audio[i] = genAudioTrack().Draw(t, fmt.Sprintf("audio_%d", i))
			audio[i].Index = i
		}

		job := domain.Job{
			Episode: domain.Episode{
				Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
			},
			Media: domain.ResolvedMedia{
				Source: domain.MediaSource{
					Kind: genMediaKind().Draw(t, "kind"),
					URL:  "https://cdn.example.com/video.m3u8",
				},
				Video:     domain.VideoTrack{Index: 0},
				Audio:     audio,
				Subtitles: nil, // Zero subtitle tracks.
			},
		}

		args := BuildFFmpegArgs(job, nil, domain.RequestAuth{}, "/tmp/out.mkv.tmp", nil)

		// No subtitle -map entries.
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-map" && strings.Contains(args[i+1], ":s") {
				t.Fatalf("found subtitle -map %q but job has no subtitles", args[i+1])
			}
		}

		// No subtitle metadata entries.
		for _, a := range args {
			if strings.HasPrefix(a, "-metadata:s:s:") {
				t.Fatalf("found subtitle metadata %q but job has no subtitles", a)
			}
		}

		// The command should still be valid (has video map, -c copy, temp path).
		foundVideoMap := false
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-map" && strings.Contains(args[i+1], ":v") {
				foundVideoMap = true
				break
			}
		}
		if !foundVideoMap {
			t.Fatal("missing video -map even with no subtitles")
		}

		if args[len(args)-1] != "/tmp/out.mkv.tmp" {
			t.Fatalf("last arg = %q, want temp path", args[len(args)-1])
		}
	})
}
