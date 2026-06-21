package outputlayout

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// **Validates: Requirements 11.2, 11.4**

// Property 28: Episode output path nesting
//
// For any root path, series title, season number [1,99], and episode number [1,99],
// the output path has exactly 3 path components after root: a series directory,
// a season directory, and a filename. The season directory contains the season number.
// The path is under root.

func TestProperty28_EpisodeOutputPathNesting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapid.StringMatching(`^/[a-z]{1,10}(/[a-z]{1,10}){0,3}$`).Draw(t, "root")
		title := rapid.StringMatching(`^[A-Za-zА-Яа-я0-9 ]{1,30}$`).Draw(t, "title")
		seriesID := rapid.StringMatching(`^[0-9]{1,6}$`).Draw(t, "seriesID")
		season := rapid.IntRange(1, 99).Draw(t, "season")
		episode := rapid.IntRange(1, 99).Draw(t, "episode")

		l := New(domain.ContainerMKV)
		series := domain.Series{
			ID:    domain.SeriesID(seriesID),
			Title: title,
		}
		ep := domain.Episode{
			Key: domain.EpisodeKey{
				Series:  domain.SeriesID(seriesID),
				Season:  season,
				Episode: episode,
			},
		}

		path, err := l.EpisodePath(root, series, ep)
		if err != nil {
			t.Fatalf("EpisodePath returned error: %v", err)
		}

		// The path must be under root.
		if !strings.HasPrefix(path, root+string(filepath.Separator)) && path != root {
			t.Fatalf("path %q is not under root %q", path, root)
		}

		// Extract the relative path after root.
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q) failed: %v", root, path, err)
		}

		// Split into components — expect exactly 3: series dir, season dir, filename.
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 {
			t.Fatalf("expected 3 path components after root, got %d: %v (path=%q, root=%q)", len(parts), parts, path, root)
		}

		seriesDir := parts[0]
		seasonDir := parts[1]
		filename := parts[2]

		// Series directory must be non-empty.
		if seriesDir == "" {
			t.Fatalf("series directory is empty")
		}

		// Season directory must contain the season number.
		seasonStr := strconv.Itoa(season)
		if !strings.Contains(seasonDir, seasonStr) {
			t.Fatalf("season directory %q does not contain season number %d", seasonDir, season)
		}

		// Filename must be non-empty.
		if filename == "" {
			t.Fatalf("filename is empty")
		}
	})
}

// Property 29: Filename season/episode round-trip and padding
//
// For any season [1,99] and episode [1,99], the filename contains S{NN}E{NN}
// with zero-padded numbers that can be parsed back to recover the original
// season and episode numbers.

var sNNeNNRegex = regexp.MustCompile(`S(\d{2,})E(\d{2,})`)

func TestProperty29_FilenameSeasonEpisodeRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		season := rapid.IntRange(1, 99).Draw(t, "season")
		episode := rapid.IntRange(1, 99).Draw(t, "episode")

		l := New(domain.ContainerMKV)
		series := domain.Series{
			ID:    "1",
			Title: "Test",
		}
		ep := domain.Episode{
			Key: domain.EpisodeKey{
				Series:  "1",
				Season:  season,
				Episode: episode,
			},
		}

		path, err := l.EpisodePath("/root", series, ep)
		if err != nil {
			t.Fatalf("EpisodePath returned error: %v", err)
		}

		filename := filepath.Base(path)

		// The filename must contain S{NN}E{NN} pattern.
		matches := sNNeNNRegex.FindStringSubmatch(filename)
		if matches == nil {
			t.Fatalf("filename %q does not contain S{NN}E{NN} pattern", filename)
		}

		// The season and episode parts must be zero-padded to at least 2 digits.
		seasonPart := matches[1]
		episodePart := matches[2]

		if len(seasonPart) < 2 {
			t.Fatalf("season part %q is not zero-padded to at least 2 digits", seasonPart)
		}
		if len(episodePart) < 2 {
			t.Fatalf("episode part %q is not zero-padded to at least 2 digits", episodePart)
		}

		// Parse back and verify round-trip.
		parsedSeason, err := strconv.Atoi(seasonPart)
		if err != nil {
			t.Fatalf("cannot parse season from %q: %v", seasonPart, err)
		}
		parsedEpisode, err := strconv.Atoi(episodePart)
		if err != nil {
			t.Fatalf("cannot parse episode from %q: %v", episodePart, err)
		}

		if parsedSeason != season {
			t.Fatalf("season round-trip failed: got %d, want %d (filename=%q)", parsedSeason, season, filename)
		}
		if parsedEpisode != episode {
			t.Fatalf("episode round-trip failed: got %d, want %d (filename=%q)", parsedEpisode, episode, filename)
		}

		// Verify the expected format S%02dE%02d.
		expectedPrefix := fmt.Sprintf("S%02dE%02d", season, episode)
		if !strings.HasPrefix(filename, expectedPrefix) {
			t.Fatalf("filename %q does not start with expected %q", filename, expectedPrefix)
		}
	})
}
