package kinopubapi

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

var itemIDRe = regexp.MustCompile(`/item/view/(\d+)`)
var anyNumRe = regexp.MustCompile(`\d+`)

// ItemIDFromURL extracts the numeric item id from a kino.pub item URL, or
// returns the input verbatim when it is already a bare numeric id.
func ItemIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := itemIDRe.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return raw
	}
	return anyNumRe.FindString(raw)
}

// ItemURL builds the canonical kino.pub item page URL for an id. This is what
// the GUI feeds into the engine as the InputURL so the API-backed scraper can
// resolve it.
func ItemURL(id string) string { return "https://kino.pub/item/view/" + id }

// Scraper adapts the kino.pub API to domain.PageScraper, so an API-sourced item
// flows into the existing HLS download pipeline with no engine changes. It
// resolves the item id from the URL, fetches details, and emits a
// domain.PagePlaylist whose episodes carry hls4 manifest URLs.
type Scraper struct {
	client *Client
	logger domain.Logger
}

// NewScraper builds an API-backed page scraper.
func NewScraper(client *Client, logger domain.Logger) *Scraper {
	return &Scraper{client: client, logger: logger}
}

var _ domain.PageScraper = (*Scraper)(nil)

// ExtractAllSeasons implements domain.PageScraper.
func (s *Scraper) ExtractAllSeasons(ctx context.Context, baseURL string) (*domain.PagePlaylist, error) {
	id := ItemIDFromURL(baseURL)
	if id == "" {
		return nil, fmt.Errorf("kino.pub API: cannot determine item id from %q", baseURL)
	}
	item, err := s.client.Item(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("resolved item via kino.pub API",
			domain.F("id", id),
			domain.F("title", item.Title),
			domain.F("type", item.Type),
		)
	}
	return BuildPagePlaylist(item)
}

// BuildPagePlaylist converts an API item into a domain.PagePlaylist. Serials map
// seasons[].episodes[]; movies map videos[] onto a single season.
func BuildPagePlaylist(item Item) (*domain.PagePlaylist, error) {
	id, _ := strconv.Atoi(item.ID.String())
	pl := &domain.PagePlaylist{
		ItemID: id,
		Title:  item.Title,
		Poster: item.Posters.Best(),
	}
	seasonCounts := map[int]int{}

	add := func(season, episode int, title string, duration int, files []File) {
		manifest := bestManifest(files)
		if manifest == "" {
			return
		}
		pl.Episodes = append(pl.Episodes, domain.PageEpisode{
			ManifestURL:  manifest,
			EpisodeTitle: title,
			Duration:     duration,
			Season:       season,
			Episode:      episode,
		})
		seasonCounts[season]++
	}

	if len(item.Seasons) > 0 {
		for _, sea := range item.Seasons {
			for _, ep := range sea.Episodes {
				title := ep.Title
				if title == "" {
					title = fmt.Sprintf("Серия %d", ep.Number)
				}
				add(sea.Number, ep.Number, title, int(ep.Duration), ep.Files)
			}
		}
	} else {
		for i, v := range item.Videos {
			epNum := v.Number
			if epNum == 0 {
				epNum = i + 1
			}
			title := v.Title
			if title == "" {
				title = item.Title
			}
			add(1, epNum, title, int(v.Duration), v.Files)
		}
	}

	if len(pl.Episodes) == 0 {
		return nil, fmt.Errorf("kino.pub API: item %s has no playable HLS files (subscription required?)", item.ID.String())
	}
	for season, count := range seasonCounts {
		pl.Seasons = append(pl.Seasons, domain.PageSeason{Season: season, Count: count})
	}
	return pl, nil
}

// bestManifest returns the HLS master URL for the highest-resolution file. When
// two files share the top resolution — kino.pub returns both an H.264 and an
// HEVC variant under mixedPlaylist (e.g. 4K) — H.264 wins: it plays in every
// browser, while HEVC needs hardware decode the browser may lack.
func bestManifest(files []File) string {
	best := ""
	bestH := -1
	bestIsAVC := false
	for _, f := range files {
		m := f.URL.Manifest()
		if m == "" {
			continue
		}
		isAVC := isAVCCodec(f.Codec)
		if f.H > bestH || (f.H == bestH && isAVC && !bestIsAVC) {
			bestH = f.H
			best = m
			bestIsAVC = isAVC
		}
	}
	return best
}

// isAVCCodec reports whether a kino.pub codec string denotes H.264/AVC.
func isAVCCodec(codec string) bool {
	c := strings.ToLower(codec)
	return strings.Contains(c, "264") || strings.Contains(c, "avc")
}
