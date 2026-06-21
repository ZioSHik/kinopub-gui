package mediaresolver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// genLanguage generates a plausible BCP-47 / ISO 639 language tag.
func genLanguage() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"eng", "rus", "ukr", "fra", "deu", "spa", "ita", "jpn", "kor", "zho"})
}

// genName generates a plausible track name (studio or subtitle source).
// Names must contain at least one non-space character since the HLS parser
// trims whitespace from attribute values.
func genName() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-zА-Яа-я0-9_\-][A-Za-zА-Яа-я0-9 _\-]{0,29}`)
}

// genGroupID generates a plausible HLS GROUP-ID.
func genGroupID() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z0-9]{1,10}`)
}

// genQuality generates a quality string like "1080p", "720p", "4k", etc.
func genQuality() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"4k", "2160p", "1440p", "1080p", "720p", "480p", "360p", "240p"})
}

// genDistinctQualities generates a slice of n distinct quality strings
// selected from the available quality pool.
func genDistinctQualities(n int) *rapid.Generator[[]string] {
	allQualities := []string{"4k", "2160p", "1440p", "1080p", "720p", "480p", "360p", "240p"}
	return rapid.Custom(func(t *rapid.T) []string {
		if n > len(allQualities) {
			n = len(allQualities)
		}
		// Create a copy and use Fisher-Yates-like selection via rapid draws.
		pool := make([]string, len(allQualities))
		copy(pool, allQualities)
		result := make([]string, n)
		for i := 0; i < n; i++ {
			idx := rapid.IntRange(0, len(pool)-1).Draw(t, fmt.Sprintf("qIdx_%d", i))
			result[i] = pool[idx]
			// Remove selected element by swapping with last.
			pool[idx] = pool[len(pool)-1]
			pool = pool[:len(pool)-1]
		}
		return result
	})
}

// audioTrackDef holds generated attributes for an audio track.
type audioTrackDef struct {
	Name     string
	Language string
	GroupID  string
}

// subtitleTrackDef holds generated attributes for a subtitle track.
type subtitleTrackDef struct {
	Name     string
	Language string
	GroupID  string
}

// genAudioTrack generates a single audio track definition.
func genAudioTrack() *rapid.Generator[audioTrackDef] {
	return rapid.Custom(func(t *rapid.T) audioTrackDef {
		return audioTrackDef{
			Name:     genName().Draw(t, "audioName"),
			Language: genLanguage().Draw(t, "audioLang"),
			GroupID:  genGroupID().Draw(t, "audioGroup"),
		}
	})
}

// genSubtitleTrack generates a single subtitle track definition.
func genSubtitleTrack() *rapid.Generator[subtitleTrackDef] {
	return rapid.Custom(func(t *rapid.T) subtitleTrackDef {
		return subtitleTrackDef{
			Name:     genName().Draw(t, "subName"),
			Language: genLanguage().Draw(t, "subLang"),
			GroupID:  genGroupID().Draw(t, "subGroup"),
		}
	})
}

// buildM3U8Playlist constructs a valid m3u8 master playlist with the given
// video variant, audio tracks, and subtitle tracks.
func buildM3U8Playlist(bandwidth int, resolution string, audioTracks []audioTrackDef, subtitleTracks []subtitleTrackDef) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bandwidth, resolution))
	b.WriteString("video.m3u8\n")

	for _, a := range audioTracks {
		b.WriteString(fmt.Sprintf(
			"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"%s\",NAME=\"%s\",LANGUAGE=\"%s\",URI=\"audio_%s.m3u8\"\n",
			a.GroupID, a.Name, a.Language, a.Language,
		))
	}

	for _, s := range subtitleTracks {
		b.WriteString(fmt.Sprintf(
			"#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"%s\",NAME=\"%s\",LANGUAGE=\"%s\",URI=\"subs_%s.m3u8\"\n",
			s.GroupID, s.Name, s.Language, s.Language,
		))
	}

	return b.String()
}

// buildFFprobeJSON constructs a valid ffprobe JSON output with the given streams.
func buildFFprobeJSON(hasVideo bool, videoWidth, videoHeight int, audioStreams []audioTrackDef) []byte {
	type tags struct {
		Language string `json:"language,omitempty"`
		Title    string `json:"title,omitempty"`
	}
	type stream struct {
		Index     int    `json:"index"`
		CodecType string `json:"codec_type"`
		Width     int    `json:"width,omitempty"`
		Height    int    `json:"height,omitempty"`
		Tags      tags   `json:"tags"`
	}

	var streams []stream
	idx := 0

	if hasVideo {
		streams = append(streams, stream{
			Index:     idx,
			CodecType: "video",
			Width:     videoWidth,
			Height:    videoHeight,
		})
		idx++
	}

	for _, a := range audioStreams {
		streams = append(streams, stream{
			Index:     idx,
			CodecType: "audio",
			Tags:      tags{Language: a.Language, Title: a.Name},
		})
		idx++
	}

	type output struct {
		Streams []stream `json:"streams"`
	}

	data, _ := json.Marshal(output{Streams: streams})
	return data
}

// ---------------------------------------------------------------------------
// Property 8: Media enumeration recovers all tracks and attributes
// ---------------------------------------------------------------------------

// For any generated m3u8 playlist with N audio tracks and M subtitle tracks
// (each with generated NAME/LANGUAGE/GROUP-ID), parsing recovers all N audio
// tracks and M subtitle tracks with their attributes intact.
func TestProperty8_MediaEnumerationRecoversAllTracks(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numAudio := rapid.IntRange(0, 10).Draw(t, "numAudio")
		numSubs := rapid.IntRange(0, 10).Draw(t, "numSubs")

		audioTracks := make([]audioTrackDef, numAudio)
		for i := range audioTracks {
			audioTracks[i] = genAudioTrack().Draw(t, fmt.Sprintf("audio_%d", i))
		}

		subtitleTracks := make([]subtitleTrackDef, numSubs)
		for i := range subtitleTracks {
			subtitleTracks[i] = genSubtitleTrack().Draw(t, fmt.Sprintf("sub_%d", i))
		}

		playlist := buildM3U8Playlist(5000000, "1920x1080", audioTracks, subtitleTracks)
		source := domain.MediaSource{Kind: domain.MediaHLS, URL: "http://example.com/master.m3u8", Quality: "1080p"}

		resolved, err := parseM3U8(strings.NewReader(playlist), source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify audio track count.
		if len(resolved.Audio) != numAudio {
			t.Fatalf("expected %d audio tracks, got %d", numAudio, len(resolved.Audio))
		}

		// Verify subtitle track count.
		if len(resolved.Subtitles) != numSubs {
			t.Fatalf("expected %d subtitle tracks, got %d", numSubs, len(resolved.Subtitles))
		}

		// Verify audio track attributes are preserved.
		for i, expected := range audioTracks {
			got := resolved.Audio[i]
			wantName := strings.TrimSpace(expected.Name)
			if got.Studio != wantName {
				t.Fatalf("audio[%d].Studio = %q, want %q", i, got.Studio, wantName)
			}
			wantLang := strings.TrimSpace(expected.Language)
			if got.Language != wantLang {
				t.Fatalf("audio[%d].Language = %q, want %q", i, got.Language, wantLang)
			}
			wantGroup := strings.TrimSpace(expected.GroupID)
			if got.GroupID != wantGroup {
				t.Fatalf("audio[%d].GroupID = %q, want %q", i, got.GroupID, wantGroup)
			}
			if got.Index != i {
				t.Fatalf("audio[%d].Index = %d, want %d", i, got.Index, i)
			}
		}

		// Verify subtitle track attributes are preserved.
		for i, expected := range subtitleTracks {
			got := resolved.Subtitles[i]
			wantName := strings.TrimSpace(expected.Name)
			if got.Source != wantName {
				t.Fatalf("subtitle[%d].Source = %q, want %q", i, got.Source, wantName)
			}
			wantLang := strings.TrimSpace(expected.Language)
			if got.Language != wantLang {
				t.Fatalf("subtitle[%d].Language = %q, want %q", i, got.Language, wantLang)
			}
			wantGroup := strings.TrimSpace(expected.GroupID)
			if got.GroupID != wantGroup {
				t.Fatalf("subtitle[%d].GroupID = %q, want %q", i, got.GroupID, wantGroup)
			}
			if got.Index != i {
				t.Fatalf("subtitle[%d].Index = %d, want %d", i, got.Index, i)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 9: Missing video is always an error
// ---------------------------------------------------------------------------

// For any m3u8 playlist that has zero #EXT-X-STREAM-INF lines (no video
// variants), parseM3U8 returns ErrNoVideoTrack.
func TestProperty9_MissingVideoM3U8(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a playlist with only audio/subtitle tracks, no video.
		numAudio := rapid.IntRange(0, 5).Draw(t, "numAudio")
		numSubs := rapid.IntRange(0, 5).Draw(t, "numSubs")

		var b strings.Builder
		b.WriteString("#EXTM3U\n")

		for i := 0; i < numAudio; i++ {
			a := genAudioTrack().Draw(t, fmt.Sprintf("audio_%d", i))
			b.WriteString(fmt.Sprintf(
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"%s\",NAME=\"%s\",LANGUAGE=\"%s\",URI=\"audio.m3u8\"\n",
				a.GroupID, a.Name, a.Language,
			))
		}

		for i := 0; i < numSubs; i++ {
			s := genSubtitleTrack().Draw(t, fmt.Sprintf("sub_%d", i))
			b.WriteString(fmt.Sprintf(
				"#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"%s\",NAME=\"%s\",LANGUAGE=\"%s\",URI=\"subs.m3u8\"\n",
				s.GroupID, s.Name, s.Language,
			))
		}

		source := domain.MediaSource{Kind: domain.MediaHLS, URL: "http://example.com/master.m3u8"}
		_, err := parseM3U8(strings.NewReader(b.String()), source)
		if err != domain.ErrNoVideoTrack {
			t.Fatalf("expected ErrNoVideoTrack, got %v", err)
		}
	})
}

// For any ffprobe output with no video codec_type stream, parseFFprobeOutput
// returns ErrNoVideoTrack.
func TestProperty9_MissingVideoFFprobe(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate ffprobe output with only audio streams (no video).
		numAudio := rapid.IntRange(0, 5).Draw(t, "numAudio")

		audioStreams := make([]audioTrackDef, numAudio)
		for i := range audioStreams {
			audioStreams[i] = genAudioTrack().Draw(t, fmt.Sprintf("audio_%d", i))
		}

		data := buildFFprobeJSON(false, 0, 0, audioStreams)
		source := domain.MediaSource{Kind: domain.MediaProgressive, URL: "http://example.com/video.mp4"}

		_, err := parseFFprobeOutput(data, source)
		if err != domain.ErrNoVideoTrack {
			t.Fatalf("expected ErrNoVideoTrack, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 10: Quality selection picks exact match else highest
// ---------------------------------------------------------------------------

// For any list of MediaSources with distinct Quality values, SelectMediaSource
// returns the exact match when pref matches one source's Quality, and returns
// the highest quality source when pref doesn't match any or is empty.
func TestProperty10_QualitySelectionExactMatch(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numSources := rapid.IntRange(2, 8).Draw(t, "numSources")
		qualities := genDistinctQualities(numSources).Draw(t, "qualities")

		sources := make([]domain.MediaSource, len(qualities))
		for i, q := range qualities {
			sources[i] = domain.MediaSource{
				Kind:    domain.MediaHLS,
				URL:     fmt.Sprintf("http://cdn.example.com/%s.m3u8", q),
				Quality: q,
			}
		}

		// Pick a random quality from the list as the preference.
		prefIdx := rapid.IntRange(0, len(qualities)-1).Draw(t, "prefIdx")
		pref := domain.QualityPref(qualities[prefIdx])

		got := SelectMediaSource(sources, pref)
		if got.Quality != string(pref) {
			t.Fatalf("SelectMediaSource with pref=%q returned Quality=%q, want exact match",
				pref, got.Quality)
		}
	})
}

func TestProperty10_QualitySelectionNoMatchPicksHighest(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numSources := rapid.IntRange(2, 8).Draw(t, "numSources")
		qualities := genDistinctQualities(numSources).Draw(t, "qualities")

		sources := make([]domain.MediaSource, len(qualities))
		for i, q := range qualities {
			sources[i] = domain.MediaSource{
				Kind:    domain.MediaHLS,
				URL:     fmt.Sprintf("http://cdn.example.com/%s.m3u8", q),
				Quality: q,
			}
		}

		// Use a preference that doesn't match any source.
		pref := domain.QualityPref("99999p")

		got := SelectMediaSource(sources, pref)

		// Find the expected highest quality.
		bestScore := -1
		bestQuality := ""
		for _, q := range qualities {
			score := qualityScore(q)
			if score > bestScore {
				bestScore = score
				bestQuality = q
			}
		}

		if got.Quality != bestQuality {
			t.Fatalf("SelectMediaSource with non-matching pref=%q returned Quality=%q, want highest=%q",
				pref, got.Quality, bestQuality)
		}
	})
}

func TestProperty10_QualitySelectionEmptyPrefPicksHighest(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numSources := rapid.IntRange(2, 8).Draw(t, "numSources")
		qualities := genDistinctQualities(numSources).Draw(t, "qualities")

		sources := make([]domain.MediaSource, len(qualities))
		for i, q := range qualities {
			sources[i] = domain.MediaSource{
				Kind:    domain.MediaHLS,
				URL:     fmt.Sprintf("http://cdn.example.com/%s.m3u8", q),
				Quality: q,
			}
		}

		got := SelectMediaSource(sources, "")

		// Find the expected highest quality.
		bestScore := -1
		bestQuality := ""
		for _, q := range qualities {
			score := qualityScore(q)
			if score > bestScore {
				bestScore = score
				bestQuality = q
			}
		}

		if got.Quality != bestQuality {
			t.Fatalf("SelectMediaSource with empty pref returned Quality=%q, want highest=%q",
				got.Quality, bestQuality)
		}
	})
}
