package pagescraper

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// playerPlaylistRe extracts the PLAYER_PLAYLIST JSON array from the page HTML.
var playerPlaylistRe = regexp.MustCompile(`window\.PLAYER_PLAYLIST\s*=\s*(\[.*?\]);`)

// playerSeasonsRe extracts the PLAYER_SEASONS JSON array from the page HTML.
var playerSeasonsRe = regexp.MustCompile(`window\.PLAYER_SEASONS\s*=\s*(\[.*?\]);`)

// playerItemIDRe extracts the PLAYER_ITEM_ID from the page HTML.
var playerItemIDRe = regexp.MustCompile(`window\.PLAYER_ITEM_ID\s*=\s*(\d+);`)

// itemSeasonEpisodeSuffixRe matches a trailing /sNeM segment in an item URL.
var itemSeasonEpisodeSuffixRe = regexp.MustCompile(`(?i)/s\d+e\d+/?$`)

// normalizeItemURL strips a trailing /sNeM episode segment from a kino.pub
// item URL so clean season URLs can be constructed.
// e.g. https://kino.pub/item/view/38290/s1e1 → https://kino.pub/item/view/38290
func normalizeItemURL(rawURL string) string {
	return itemSeasonEpisodeSuffixRe.ReplaceAllString(rawURL, "")
}

// PlayerEpisode represents a single episode entry from PLAYER_PLAYLIST.
type PlayerEpisode struct {
	Manifest     string `json:"manifest"`
	ID           int    `json:"id"`
	MediaID      int    `json:"media_id"`
	Title        string `json:"title"`
	EpisodeTitle string `json:"episode_title"`
	Poster       string `json:"poster"`
	Thumb        string `json:"thumb"`
	Duration     int    `json:"duration"` // seconds
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
}

// PlayerSeason represents a season entry from PLAYER_SEASONS.
type PlayerSeason struct {
	Season   int  `json:"season"`
	SeasonID int  `json:"season_id"`
	Count    int  `json:"count"`
}

// PagePlaylist holds the full extracted playlist data from a kino.pub page.
type PagePlaylist struct {
	ItemID   int
	Title    string // series title (from first episode)
	Poster   string
	Episodes []PlayerEpisode
	Seasons  []PlayerSeason
}

// toDomain converts the internal PagePlaylist to the domain type.
func (p *PagePlaylist) toDomain() *domain.PagePlaylist {
	result := &domain.PagePlaylist{
		ItemID: p.ItemID,
		Title:  p.Title,
		Poster: p.Poster,
	}

	for _, ep := range p.Episodes {
		result.Episodes = append(result.Episodes, domain.PageEpisode{
			ManifestURL:  ep.Manifest,
			MediaID:      ep.MediaID,
			EpisodeTitle: ep.EpisodeTitle,
			Duration:     ep.Duration,
			Season:       ep.Season,
			Episode:      ep.Episode,
		})
	}

	for _, s := range p.Seasons {
		result.Seasons = append(result.Seasons, domain.PageSeason{
			Season: s.Season,
			Count:  s.Count,
		})
	}

	return result
}

// ExtractPlaylist fetches a kino.pub page and extracts the PLAYER_PLAYLIST
// and PLAYER_SEASONS data. This provides HLS manifest URLs for all episodes
// in the current season, plus season metadata for navigating to other seasons.
//
// The pageURL should be a kino.pub item page, e.g.:
//   https://kino.pub/item/view/38290
//   https://kino.pub/item/view/38290/s1e1
func (s *Scraper) ExtractPlaylist(ctx context.Context, pageURL string) (*PagePlaylist, error) {
	return s.extractPlaylistInternal(ctx, pageURL)
}

// extractPlaylistInternal is the internal implementation.
func (s *Scraper) extractPlaylistInternal(ctx context.Context, pageURL string) (*PagePlaylist, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	body, err := s.fetchPage(ctx, pageURL)
	if err != nil {
		return nil, err
	}

	return s.parsePlaylist(body)
}

// ExtractAllSeasons fetches playlists for all seasons of a series.
// It loads the initial page to get season info, then fetches each season's
// page to get its episodes. Returns domain.PagePlaylist.
func (s *Scraper) ExtractAllSeasons(ctx context.Context, baseURL string) (*domain.PagePlaylist, error) {
	// Normalize the base URL: strip any trailing /sNeM episode segment so we
	// can construct clean season URLs. e.g.
	//   https://kino.pub/item/view/38290/s1e1 → https://kino.pub/item/view/38290
	baseURL = normalizeItemURL(baseURL)

	// First, load the base page to get season list.
	initial, err := s.extractPlaylistInternal(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("initial page: %w", err)
	}

	if len(initial.Seasons) <= 1 {
		// Single season — we already have everything.
		return initial.toDomain(), nil
	}

	// Check which season we already have.
	haveSeason := 0
	if len(initial.Episodes) > 0 {
		haveSeason = initial.Episodes[0].Season
	}

	// Fetch other seasons. Deduplicate by (season, episode) in case the CDN
	// returns overlapping data.
	seen := make(map[[2]int]bool)
	allEpisodes := make([]PlayerEpisode, 0, len(initial.Episodes))
	for _, ep := range initial.Episodes {
		key := [2]int{ep.Season, ep.Episode}
		if !seen[key] {
			seen[key] = true
			allEpisodes = append(allEpisodes, ep)
		}
	}

	for _, season := range initial.Seasons {
		if season.Season == haveSeason {
			continue // already have this one
		}

		// Construct URL for this season: /item/view/{id}/s{N}e1
		seasonURL := fmt.Sprintf("%s/s%de1", baseURL, season.Season)
		s.log.Info("fetching season playlist",
			domain.F("season", season.Season),
			domain.F("url", seasonURL),
		)

		seasonPlaylist, err := s.extractPlaylistInternal(ctx, seasonURL)
		if err != nil {
			s.log.Warn("failed to fetch season, skipping",
				domain.F("season", season.Season),
				domain.F("error", err.Error()),
			)
			continue
		}

		// Verify we got the right season — guard against the page returning
		// season 1 again for an invalid URL.
		gotRightSeason := false
		for _, ep := range seasonPlaylist.Episodes {
			if ep.Season == season.Season {
				gotRightSeason = true
			}
			key := [2]int{ep.Season, ep.Episode}
			if !seen[key] {
				seen[key] = true
				allEpisodes = append(allEpisodes, ep)
			}
		}
		if !gotRightSeason {
			s.log.Warn("season page returned unexpected season, may be incomplete",
				domain.F("requested_season", season.Season),
			)
		}
	}

	initial.Episodes = allEpisodes
	return initial.toDomain(), nil
}

// parsePlaylist extracts PLAYER_PLAYLIST and PLAYER_SEASONS from HTML body.
func (s *Scraper) parsePlaylist(body []byte) (*PagePlaylist, error) {
	result := &PagePlaylist{}

	// Extract PLAYER_ITEM_ID.
	if m := playerItemIDRe.FindSubmatch(body); m != nil {
		fmt.Sscanf(string(m[1]), "%d", &result.ItemID)
	}

	// Extract PLAYER_PLAYLIST.
	playlistMatch := playerPlaylistRe.FindSubmatch(body)
	if playlistMatch == nil {
		return nil, fmt.Errorf("PLAYER_PLAYLIST not found in page (auth may be required)")
	}

	var episodes []PlayerEpisode
	if err := json.Unmarshal(playlistMatch[1], &episodes); err != nil {
		return nil, fmt.Errorf("parse PLAYER_PLAYLIST: %w", err)
	}
	result.Episodes = episodes

	// Extract title and poster from first episode.
	if len(episodes) > 0 {
		result.Title = episodes[0].Title
		result.Poster = episodes[0].Poster
	}

	// Extract PLAYER_SEASONS.
	seasonsMatch := playerSeasonsRe.FindSubmatch(body)
	if seasonsMatch != nil {
		var seasons []PlayerSeason
		if err := json.Unmarshal(seasonsMatch[1], &seasons); err == nil {
			result.Seasons = seasons
		}
	}

	s.log.Info("extracted playlist from page",
		domain.F("item_id", result.ItemID),
		domain.F("episodes", len(result.Episodes)),
		domain.F("seasons", len(result.Seasons)),
	)

	return result, nil
}

// Verify that *Scraper satisfies domain.PageScraper at compile time.
var _ domain.PageScraper = (*Scraper)(nil)
