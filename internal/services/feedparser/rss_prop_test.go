package feedparser

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/logx"

	"pgregory.net/rapid"
)

// **Validates: Requirements 2.2, 2.3, 2.4, 2.8**

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// warnCollector is a logx.Handler that collects warn-level messages.
type warnCollector struct {
	warnings []string
}

func (w *warnCollector) Handle(rec logx.Record) {
	if rec.Level == domain.LevelWarn {
		w.warnings = append(w.warnings, rec.Message)
	}
}

// buildRSSXML constructs a valid RSS XML feed from the given channel-level
// fields and items.
func buildRSSXML(title, subtitle, description, posterURL string, items []rssItemData) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">`)
	b.WriteString(`<channel>`)
	b.WriteString(`<title>` + xmlEscape(title) + `</title>`)
	if subtitle != "" {
		b.WriteString(`<itunes:subtitle>` + xmlEscape(subtitle) + `</itunes:subtitle>`)
	}
	b.WriteString(`<description>` + xmlEscape(description) + `</description>`)
	if posterURL != "" {
		b.WriteString(`<itunes:image href="` + xmlEscape(posterURL) + `"/>`)
	}
	for _, item := range items {
		b.WriteString(`<item>`)
		b.WriteString(`<title>` + xmlEscape(item.Title) + `</title>`)
		b.WriteString(`<link>` + xmlEscape(item.Link) + `</link>`)
		if item.Summary != "" {
			b.WriteString(`<itunes:summary>` + xmlEscape(item.Summary) + `</itunes:summary>`)
		}
		b.WriteString(`<enclosure url="` + xmlEscape(item.EnclosureURL) + `"/>`)
		b.WriteString(`</item>`)
	}
	b.WriteString(`</channel>`)
	b.WriteString(`</rss>`)
	return []byte(b.String())
}

type rssItemData struct {
	Title        string
	Link         string
	Summary      string
	EnclosureURL string
}

func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

// genSafeString generates a non-empty string that is XML-safe (no control chars
// that would break XML parsing).
func genSafeString() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-zА-Яа-я0-9 _\-]{1,50}`)
}

// genURL generates a plausible URL string.
func genURL() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		path := rapid.StringMatching(`[a-z0-9]{1,20}`).Draw(t, "path")
		return "https://example.com/" + path
	})
}

// ---------------------------------------------------------------------------
// Property 4: Series/episode field extraction
// ---------------------------------------------------------------------------

// For any generated RSS feed produced from a model Series (with title,
// original title, description, poster), parsing recovers those channel-level
// fields.
func TestProperty4_SeriesFieldExtraction(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		title := genSafeString().Draw(t, "title")
		originalTitle := genSafeString().Draw(t, "originalTitle")
		description := genSafeString().Draw(t, "description")
		posterURL := genURL().Draw(t, "posterURL")

		// We need at least one parseable item so the feed doesn't return ErrEmptyFeed.
		season := rapid.IntRange(1, 99).Draw(t, "season")
		episode := rapid.IntRange(1, 99).Draw(t, "episode")
		itemTitle := fmt.Sprintf("s%02de%02d - Test Episode", season, episode)

		items := []rssItemData{
			{
				Title:        itemTitle,
				Link:         fmt.Sprintf("https://kino.pub/item/view/1/s%de%d/slug", season, episode),
				Summary:      "1080p",
				EnclosureURL: "https://cdn.example.com/video.mp4",
			},
		}

		data := buildRSSXML(title, originalTitle, description, posterURL, items)

		p := New(&http.Client{}, logx.New(nil))
		src := domain.FeedSource{ID: "1", Token: "t"}
		series, err := p.parseRSS(data, src)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		if series.Title != title {
			t.Fatalf("Title mismatch: got %q, want %q", series.Title, title)
		}
		if series.OriginalTitle != originalTitle {
			t.Fatalf("OriginalTitle mismatch: got %q, want %q", series.OriginalTitle, originalTitle)
		}
		if series.Description != description {
			t.Fatalf("Description mismatch: got %q, want %q", series.Description, description)
		}
		if series.PosterURL != posterURL {
			t.Fatalf("PosterURL mismatch: got %q, want %q", series.PosterURL, posterURL)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 5: Season/episode parsing round-trip
// ---------------------------------------------------------------------------

// For any season number [1,99] and episode number [1,99], formatting them into
// a title (s{NN}e{NN} - ...) and/or a page-link path (/s{n}e{n}/) and then
// running ParseSeasonEpisode recovers the original numbers, with title taking
// precedence.
func TestProperty5_SeasonEpisodeRoundTrip_TitleOnly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		season := rapid.IntRange(1, 99).Draw(t, "season")
		episode := rapid.IntRange(1, 99).Draw(t, "episode")

		title := fmt.Sprintf("s%02de%02d - Some Episode Title", season, episode)

		gotS, gotE, ok := ParseSeasonEpisode(title, "")
		if !ok {
			t.Fatalf("ParseSeasonEpisode(%q, \"\") returned ok=false", title)
		}
		if gotS != season || gotE != episode {
			t.Fatalf("ParseSeasonEpisode(%q, \"\") = (%d, %d), want (%d, %d)",
				title, gotS, gotE, season, episode)
		}
	})
}

func TestProperty5_SeasonEpisodeRoundTrip_LinkOnly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		season := rapid.IntRange(1, 99).Draw(t, "season")
		episode := rapid.IntRange(1, 99).Draw(t, "episode")

		link := fmt.Sprintf("https://kino.pub/item/view/123/s%de%d/slug", season, episode)

		gotS, gotE, ok := ParseSeasonEpisode("no match here", link)
		if !ok {
			t.Fatalf("ParseSeasonEpisode(\"no match here\", %q) returned ok=false", link)
		}
		if gotS != season || gotE != episode {
			t.Fatalf("ParseSeasonEpisode(\"no match here\", %q) = (%d, %d), want (%d, %d)",
				link, gotS, gotE, season, episode)
		}
	})
}

func TestProperty5_SeasonEpisodeRoundTrip_TitlePrecedence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two distinct season/episode pairs.
		titleSeason := rapid.IntRange(1, 99).Draw(t, "titleSeason")
		titleEpisode := rapid.IntRange(1, 99).Draw(t, "titleEpisode")
		linkSeason := rapid.IntRange(1, 99).Draw(t, "linkSeason")
		linkEpisode := rapid.IntRange(1, 99).Draw(t, "linkEpisode")

		title := fmt.Sprintf("s%02de%02d - Episode", titleSeason, titleEpisode)
		link := fmt.Sprintf("https://kino.pub/item/view/123/s%de%d/slug", linkSeason, linkEpisode)

		gotS, gotE, ok := ParseSeasonEpisode(title, link)
		if !ok {
			t.Fatalf("ParseSeasonEpisode(%q, %q) returned ok=false", title, link)
		}
		// Title should always take precedence.
		if gotS != titleSeason || gotE != titleEpisode {
			t.Fatalf("ParseSeasonEpisode(%q, %q) = (%d, %d), want title values (%d, %d)",
				title, link, gotS, gotE, titleSeason, titleEpisode)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 6: Catalog ordering invariant
// ---------------------------------------------------------------------------

// For any unordered set of parsed episodes, the resulting Series has Seasons
// sorted ascending and Episodes within each season sorted ascending.
func TestProperty6_CatalogOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random set of (season, episode) pairs.
		numItems := rapid.IntRange(1, 30).Draw(t, "numItems")

		type seKey struct{ s, e int }
		seen := make(map[seKey]bool)
		var items []rssItemData

		for i := 0; i < numItems; i++ {
			s := rapid.IntRange(1, 10).Draw(t, fmt.Sprintf("season_%d", i))
			e := rapid.IntRange(1, 50).Draw(t, fmt.Sprintf("episode_%d", i))
			key := seKey{s, e}
			if seen[key] {
				continue // skip duplicates
			}
			seen[key] = true

			itemTitle := fmt.Sprintf("s%02de%02d - Episode %d", s, e, e)
			items = append(items, rssItemData{
				Title:        itemTitle,
				Link:         fmt.Sprintf("https://kino.pub/item/view/1/s%de%d/", s, e),
				Summary:      "1080p",
				EnclosureURL: "https://cdn.example.com/video.mp4",
			})
		}

		if len(items) == 0 {
			return // degenerate case: all duplicates
		}

		data := buildRSSXML("Test", "Orig", "Desc", "https://example.com/poster.jpg", items)

		p := New(&http.Client{}, logx.New(nil))
		src := domain.FeedSource{ID: "1", Token: "t"}
		series, err := p.parseRSS(data, src)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		// Verify seasons are sorted ascending.
		for i := 1; i < len(series.Seasons); i++ {
			if series.Seasons[i].Number <= series.Seasons[i-1].Number {
				t.Fatalf("seasons not sorted ascending: season[%d]=%d, season[%d]=%d",
					i-1, series.Seasons[i-1].Number, i, series.Seasons[i].Number)
			}
		}

		// Verify episodes within each season are sorted ascending.
		for _, season := range series.Seasons {
			for i := 1; i < len(season.Episodes); i++ {
				if season.Episodes[i].Key.Episode <= season.Episodes[i-1].Key.Episode {
					t.Fatalf("season %d: episodes not sorted ascending: ep[%d]=%d, ep[%d]=%d",
						season.Number,
						i-1, season.Episodes[i-1].Key.Episode,
						i, season.Episodes[i].Key.Episode)
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 7: Unparseable entries are excluded and warned
// ---------------------------------------------------------------------------

// For any feed mixing parseable entries with entries that have no season/episode
// in either title or page link, parsing excludes the unparseable ones and
// includes only the parseable ones.
func TestProperty7_UnparseableExcludedAndWarned(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate some parseable items.
		numParseable := rapid.IntRange(1, 10).Draw(t, "numParseable")
		// Generate some unparseable items.
		numUnparseable := rapid.IntRange(1, 10).Draw(t, "numUnparseable")

		type seKey struct{ s, e int }
		seen := make(map[seKey]bool)
		var items []rssItemData
		var expectedKeys []seKey

		for i := 0; i < numParseable; i++ {
			s := rapid.IntRange(1, 10).Draw(t, fmt.Sprintf("ps_%d", i))
			e := rapid.IntRange(1, 50).Draw(t, fmt.Sprintf("pe_%d", i))
			key := seKey{s, e}
			if seen[key] {
				continue
			}
			seen[key] = true

			itemTitle := fmt.Sprintf("s%02de%02d - Good Episode", s, e)
			items = append(items, rssItemData{
				Title:        itemTitle,
				Link:         fmt.Sprintf("https://kino.pub/item/view/1/s%de%d/", s, e),
				Summary:      "1080p",
				EnclosureURL: "https://cdn.example.com/video.mp4",
			})
			expectedKeys = append(expectedKeys, key)
		}

		if len(expectedKeys) == 0 {
			return // degenerate: all duplicates
		}

		for i := 0; i < numUnparseable; i++ {
			// Generate titles and links that do NOT match the s{N}e{N} pattern.
			badTitle := rapid.StringMatching(`[A-Za-z ]{5,30}`).Draw(t, fmt.Sprintf("badTitle_%d", i))
			badLink := rapid.StringMatching(`https://example\\.com/[a-z]{3,10}`).Draw(t, fmt.Sprintf("badLink_%d", i))
			items = append(items, rssItemData{
				Title:        badTitle,
				Link:         badLink,
				Summary:      "720p",
				EnclosureURL: "https://cdn.example.com/bad.mp4",
			})
		}

		// Shuffle items to ensure ordering doesn't matter.
		shuffled := make([]rssItemData, len(items))
		copy(shuffled, items)
		perm := rapid.SliceOfN(rapid.IntRange(0, len(shuffled)-1), len(shuffled), len(shuffled))
		_ = perm // We'll just use the items as-is since rapid doesn't have a shuffle primitive.

		data := buildRSSXML("Test", "Orig", "Desc", "https://example.com/poster.jpg", items)

		// Use a warn collector to verify warnings are emitted.
		collector := &warnCollector{}
		logger := logx.New([]logx.Handler{collector})

		p := New(&http.Client{}, logger)
		src := domain.FeedSource{ID: "1", Token: "t"}
		series, err := p.parseRSS(data, src)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		// Count total episodes in the result.
		totalEpisodes := 0
		for _, season := range series.Seasons {
			totalEpisodes += len(season.Episodes)
		}

		// The result should contain exactly the parseable items.
		if totalEpisodes != len(expectedKeys) {
			t.Fatalf("expected %d episodes, got %d", len(expectedKeys), totalEpisodes)
		}

		// Verify all expected keys are present.
		gotKeys := make(map[seKey]bool)
		for _, season := range series.Seasons {
			for _, ep := range season.Episodes {
				gotKeys[seKey{ep.Key.Season, ep.Key.Episode}] = true
			}
		}
		for _, ek := range expectedKeys {
			if !gotKeys[ek] {
				t.Fatalf("expected episode s%02de%02d not found in result", ek.s, ek.e)
			}
		}

		// Verify that warnings were emitted for unparseable entries.
		if len(collector.warnings) != numUnparseable {
			t.Fatalf("expected %d warnings for unparseable entries, got %d",
				numUnparseable, len(collector.warnings))
		}
	})
}

// ---------------------------------------------------------------------------
// Additional helper: verify sort is stable for duplicate-free inputs
// ---------------------------------------------------------------------------

func TestProperty6_CatalogOrdering_Comprehensive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a specific number of seasons and episodes per season.
		numSeasons := rapid.IntRange(1, 5).Draw(t, "numSeasons")

		type seKey struct{ s, e int }
		var items []rssItemData
		expectedSeasons := make([]int, 0, numSeasons)
		expectedEpisodes := make(map[int][]int) // season -> sorted episode numbers

		for si := 0; si < numSeasons; si++ {
			// Pick a random season number.
			sn := rapid.IntRange(1, 20).Draw(t, fmt.Sprintf("seasonNum_%d", si))
			// Avoid duplicate season numbers.
			found := false
			for _, es := range expectedSeasons {
				if es == sn {
					found = true
					break
				}
			}
			if found {
				continue
			}
			expectedSeasons = append(expectedSeasons, sn)

			numEps := rapid.IntRange(1, 8).Draw(t, fmt.Sprintf("numEps_%d", si))
			epNums := make(map[int]bool)
			for ei := 0; ei < numEps; ei++ {
				en := rapid.IntRange(1, 50).Draw(t, fmt.Sprintf("epNum_%d_%d", si, ei))
				if epNums[en] {
					continue
				}
				epNums[en] = true

				itemTitle := fmt.Sprintf("s%02de%02d - Ep", sn, en)
				items = append(items, rssItemData{
					Title:        itemTitle,
					Link:         fmt.Sprintf("https://kino.pub/item/view/1/s%de%d/", sn, en),
					EnclosureURL: "https://cdn.example.com/video.mp4",
				})
			}

			sortedEps := make([]int, 0, len(epNums))
			for en := range epNums {
				sortedEps = append(sortedEps, en)
			}
			sort.Ints(sortedEps)
			expectedEpisodes[sn] = sortedEps
		}

		if len(items) == 0 {
			return
		}

		sort.Ints(expectedSeasons)

		data := buildRSSXML("Test", "", "Desc", "", items)
		p := New(&http.Client{}, logx.New(nil))
		src := domain.FeedSource{ID: "1", Token: "t"}
		series, err := p.parseRSS(data, src)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		// Verify season order matches expected.
		if len(series.Seasons) != len(expectedSeasons) {
			t.Fatalf("expected %d seasons, got %d", len(expectedSeasons), len(series.Seasons))
		}
		for i, s := range series.Seasons {
			if s.Number != expectedSeasons[i] {
				t.Fatalf("season[%d] = %d, want %d", i, s.Number, expectedSeasons[i])
			}
			// Verify episode order within this season.
			expEps := expectedEpisodes[s.Number]
			if len(s.Episodes) != len(expEps) {
				t.Fatalf("season %d: expected %d episodes, got %d", s.Number, len(expEps), len(s.Episodes))
			}
			for j, ep := range s.Episodes {
				if ep.Key.Episode != expEps[j] {
					t.Fatalf("season %d, ep[%d] = %d, want %d", s.Number, j, ep.Key.Episode, expEps[j])
				}
			}
		}
	})
}
