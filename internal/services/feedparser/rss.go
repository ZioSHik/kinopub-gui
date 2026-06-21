// Package feedparser implements the domain.FeedParser interface by retrieving
// and parsing a kino.pub tokenized podcast RSS feed into a domain.Series catalog.
package feedparser

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// feedURL constructs the full RSS feed URL from a FeedSource.
func feedURL(src domain.FeedSource) string {
	return "https://kino.pub/podcast/get/" + src.ID + "/" + src.Token
}

// Parser implements domain.FeedParser.
type Parser struct {
	client *http.Client
	log    domain.Logger
}

// New creates a new feed parser with the given HTTP client and logger.
// The client should already carry any proxy configuration.
func New(client *http.Client, log domain.Logger) *Parser {
	return &Parser{
		client: client,
		log:    log.Component("feedparser"),
	}
}

// Parse retrieves and parses the RSS feed into a Series (Req 2.1, 2.2).
// Entries whose season/episode cannot be determined are excluded with a warn
// log (Req 2.8). Returns ErrEmptyFeed when zero episodes parse (Req 2.6),
// and descriptive errors for retrieval/parse failures (Req 2.5, 2.7).
//
// When src.LocalPath is set, the feed is read from that file instead of being
// fetched over the network — useful when the feed URL returns 403.
func (p *Parser) Parse(ctx context.Context, src domain.FeedSource) (domain.Series, error) {
	if src.LocalPath != "" {
		return p.parseLocalFile(src)
	}

	url := feedURL(src)
	p.log.Info("retrieving feed", domain.F("url", url))

	// Apply 30s retrieval timeout (Req 2.1).
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Series{}, fmt.Errorf("%w: %v", domain.ErrFeedRetrieval, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return domain.Series{}, fmt.Errorf("%w: %v", domain.ErrFeedRetrieval, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return domain.Series{}, fmt.Errorf("%w: HTTP %d", domain.ErrFeedRetrieval, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.Series{}, fmt.Errorf("%w: reading body: %v", domain.ErrFeedRetrieval, err)
	}

	return p.parseRSS(body, src)
}

// parseLocalFile reads and parses an RSS feed from a local file path.
func (p *Parser) parseLocalFile(src domain.FeedSource) (domain.Series, error) {
	p.log.Info("reading feed from local file", domain.F("path", src.LocalPath))

	body, err := os.ReadFile(src.LocalPath)
	if err != nil {
		return domain.Series{}, fmt.Errorf("%w: reading feed file: %v", domain.ErrFeedRetrieval, err)
	}

	// When no explicit ID was provided (no URL), derive one from the filename
	// so the state store has a stable key.
	if src.ID == "" {
		base := filepath.Base(src.LocalPath)
		ext := filepath.Ext(base)
		src.ID = strings.TrimSuffix(base, ext)
	}

	return p.parseRSS(body, src)
}

// parseRSS decodes the RSS XML and builds the domain.Series.
func (p *Parser) parseRSS(data []byte, src domain.FeedSource) (domain.Series, error) {
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return domain.Series{}, fmt.Errorf("%w: %v", domain.ErrFeedParse, err)
	}

	ch := feed.Channel

	series := domain.Series{
		ID:            domain.SeriesID(src.ID),
		Title:         ch.Title,
		OriginalTitle: ch.originalTitle(),
		Description:   ch.Description,
		PosterURL:     ch.Image.Href,
	}

	// Parse items into episodes, grouping by season.
	seasonMap := make(map[int][]domain.Episode)

	for _, item := range ch.Items {
		season, episode, ok := ParseSeasonEpisode(item.Title, item.Link)
		if !ok {
			p.log.Warn("excluding entry: cannot determine season/episode",
				domain.F("title", item.Title),
				domain.F("link", item.Link),
			)
			continue
		}

		mediaSource := classifyEnclosure(item.Enclosure.URL)

		ep := domain.Episode{
			Key: domain.EpisodeKey{
				Series:  series.ID,
				Season:  season,
				Episode: episode,
			},
			Title:        item.Title,
			Quality:      item.Summary,
			PageLink:     item.Link,
			MediaSources: []domain.MediaSource{mediaSource},
		}

		seasonMap[season] = append(seasonMap[season], ep)
	}

	if len(seasonMap) == 0 {
		return domain.Series{}, domain.ErrEmptyFeed
	}

	// Count total episodes.
	totalEpisodes := 0
	for _, eps := range seasonMap {
		totalEpisodes += len(eps)
	}
	if totalEpisodes == 0 {
		return domain.Series{}, domain.ErrEmptyFeed
	}

	// Build sorted seasons (Req 2.4).
	seasonNums := make([]int, 0, len(seasonMap))
	for n := range seasonMap {
		seasonNums = append(seasonNums, n)
	}
	sort.Ints(seasonNums)

	for _, sn := range seasonNums {
		eps := seasonMap[sn]
		sort.Slice(eps, func(i, j int) bool {
			return eps[i].Key.Episode < eps[j].Key.Episode
		})
		series.Seasons = append(series.Seasons, domain.Season{
			Number:   sn,
			Episodes: eps,
		})
	}

	p.log.Info("feed parsed",
		domain.F("title", series.Title),
		domain.F("seasons", len(series.Seasons)),
		domain.F("episodes", totalEpisodes),
	)

	return series, nil
}

// ---------------------------------------------------------------------------
// Season/episode parsing
// ---------------------------------------------------------------------------

// seRegex is the primary regex for extracting season/episode from a title.
var seRegex = regexp.MustCompile(`(?i)s(\d+)e(\d+)`)

// seLinkRegex is the fallback regex for extracting season/episode from a page-link path.
var seLinkRegex = regexp.MustCompile(`(?i)/s(\d+)e(\d+)`)

// ParseSeasonEpisode extracts season and episode numbers from a title string,
// falling back to the page-link path. Returns (season, episode, ok).
func ParseSeasonEpisode(title, pageLink string) (season, episode int, ok bool) {
	// Primary: try title.
	if m := seRegex.FindStringSubmatch(title); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}

	// Fallback: try page-link path.
	if m := seLinkRegex.FindStringSubmatch(pageLink); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}

	return 0, 0, false
}

// ---------------------------------------------------------------------------
// Enclosure classification
// ---------------------------------------------------------------------------

// classifyEnclosure determines the MediaKind from the enclosure URL.
func classifyEnclosure(url string) domain.MediaSource {
	kind := domain.MediaProgressive
	if strings.HasSuffix(strings.ToLower(url), ".m3u8") {
		kind = domain.MediaHLS
	}
	return domain.MediaSource{
		Kind: kind,
		URL:  url,
	}
}

// ---------------------------------------------------------------------------
// RSS XML structures
// ---------------------------------------------------------------------------

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Subtitle    string    `xml:"subtitle"`
	Author      string    `xml:"author"`
	Description string    `xml:"description"`
	Image       rssImage  `xml:"image"`
	Items       []rssItem `xml:"item"`
}

// originalTitle returns the original title from itunes:subtitle or itunes:author.
func (ch *rssChannel) originalTitle() string {
	if ch.Subtitle != "" {
		return ch.Subtitle
	}
	return ch.Author
}

type rssImage struct {
	Href string `xml:"href,attr"`
}

type rssItem struct {
	Title     string       `xml:"title"`
	Link      string       `xml:"link"`
	Summary   string       `xml:"summary"`
	Enclosure rssEnclosure `xml:"enclosure"`
}

type rssEnclosure struct {
	URL string `xml:"url,attr"`
}

// Verify that *Parser satisfies domain.FeedParser at compile time.
var _ domain.FeedParser = (*Parser)(nil)
