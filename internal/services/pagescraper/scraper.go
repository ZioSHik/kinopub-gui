// Package pagescraper fetches a kino.pub item page and extracts the podcast
// feed URL from the HTML. This allows the tool to accept plain page links
// (e.g. https://kino.pub/item/view/38290/s1e1) and automatically resolve
// them to the tokenized podcast feed URL, provided valid authentication
// cookies are supplied.
package pagescraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// podcastHrefRe matches the podcast feed link in the page HTML.
// It captures the numeric ID and the token from the href attribute.
// Example: href="/podcast/get/38290/Q2o2DhcYJMP-1SW9cIRUdK0oRnXo4AESCNjRKB_R81s4QS0pljKmWuB1BtlfgxZs"
var podcastHrefRe = regexp.MustCompile(`href="/podcast/get/(\d+)/([^"]+)"`)

// Scraper fetches a kino.pub page and extracts the podcast feed link.
type Scraper struct {
	client *http.Client
	log    domain.Logger
}

// New creates a Scraper. The HTTP client should already carry authentication
// headers (Cookie, User-Agent) via the httpx.WithAuth wrapper.
func New(client *http.Client, log domain.Logger) *Scraper {
	return &Scraper{client: client, log: log.Component("pagescraper")}
}

// ExtractFeedSource fetches the given kino.pub page URL and parses the podcast
// feed link from the HTML response. Returns ErrFeedTokenUnavailable if the page
// does not contain a podcast link (e.g. user is not authenticated), or
// ErrAuthRequired if the server returns 403.
func (s *Scraper) ExtractFeedSource(ctx context.Context, pageURL string) (domain.FeedSource, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	body, err := s.fetchPage(ctx, pageURL)
	if err != nil {
		return domain.FeedSource{}, err
	}

	return s.parseBody(body, pageURL)
}

// fetchPage fetches a kino.pub page and returns the body bytes.
func (s *Scraper) fetchPage(ctx context.Context, pageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	s.log.Debug("fetching page", domain.F("url", pageURL))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		s.log.Warn("page returned 403 — authentication required",
			domain.F("url", pageURL),
			domain.F("status", resp.StatusCode),
		)
		return nil, domain.ErrAuthRequired
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, pageURL)
	}

	// Read the body (limit to 2MB to avoid unbounded memory usage).
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return body, nil
}

// parseBody extracts the podcast feed link from the HTML body bytes.
func (s *Scraper) parseBody(body []byte, pageURL string) (domain.FeedSource, error) {
	matches := podcastHrefRe.FindSubmatch(body)
	if matches == nil {
		s.log.Warn("no podcast link found in page — user may not be authenticated",
			domain.F("url", pageURL),
			domain.F("body_size", len(body)),
		)
		return domain.FeedSource{}, domain.ErrFeedTokenUnavailable
	}

	id := string(matches[1])
	token := string(matches[2])

	s.log.Info("extracted podcast feed from page",
		domain.F("id", id),
		domain.F("token_prefix", truncate(token, 8)),
	)

	return domain.FeedSource{
		ID:    id,
		Token: token,
	}, nil
}

// truncate returns at most n characters of s, appending "…" if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
