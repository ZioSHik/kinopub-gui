// Package inputresolver classifies and resolves kino.pub URLs into feed sources.
package inputresolver

import (
	"context"
	"net/url"
	"regexp"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Compile-time interface satisfaction check.
var _ domain.InputResolver = (*Resolver)(nil)

// Compiled path patterns for URL classification.
var (
	// /podcast/get/{id}/{token}
	podcastFeedRe = regexp.MustCompile(`^/podcast/get/(\d+)/([^/]+)$`)
	// /item/view/{id} optionally followed by a slug segment
	pageLinkeRe = regexp.MustCompile(`^/item/view/(\d+)(?:/[^/]*)?$`)
)

// PageScraper extracts a FeedSource from a kino.pub page by fetching its HTML
// and parsing the embedded podcast link. This is an optional dependency: when
// nil, page link resolution falls back to ErrFeedTokenUnavailable.
type PageScraper interface {
	ExtractFeedSource(ctx context.Context, pageURL string) (domain.FeedSource, error)
}

// Resolver implements domain.InputResolver.
type Resolver struct {
	log     domain.Logger
	scraper PageScraper
}

// New creates a new Resolver. The logger is used for diagnostic output.
// The scraper is optional — pass nil to disable page link resolution.
func New(log domain.Logger, opts ...Option) *Resolver {
	r := &Resolver{log: log}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Option configures the Resolver.
type Option func(*Resolver)

// WithPageScraper sets the page scraper used to resolve page links into feed
// sources. When set, page links (https://kino.pub/item/view/...) are fetched
// and the podcast feed URL is extracted from the HTML.
func WithPageScraper(s PageScraper) Option {
	return func(r *Resolver) { r.scraper = s }
}

// Classify inspects a raw URL string and returns its InputClass.
// It returns ErrInvalidInputURL for empty, non-HTTP(S), wrong-host, or
// unclassified URLs.
func (r *Resolver) Classify(rawURL string) (domain.InputClass, error) {
	if rawURL == "" {
		return domain.ClassUnclassified, domain.ErrInvalidInputURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return domain.ClassUnclassified, domain.ErrInvalidInputURL
	}

	// Must be http or https scheme.
	if u.Scheme != "http" && u.Scheme != "https" {
		return domain.ClassUnclassified, domain.ErrInvalidInputURL
	}

	// Host must be kino.pub (with or without port).
	host := u.Hostname()
	if host != "kino.pub" {
		return domain.ClassUnclassified, domain.ErrInvalidInputURL
	}

	// Match path against known patterns.
	if podcastFeedRe.MatchString(u.Path) {
		return domain.ClassPodcastFeed, nil
	}
	if pageLinkeRe.MatchString(u.Path) {
		return domain.ClassPageLink, nil
	}

	// Unclassified path on kino.pub → invalid.
	return domain.ClassUnclassified, domain.ErrInvalidInputURL
}

// Resolve produces a FeedSource from the given URL. For podcast feed URLs it
// extracts the ID and token directly. For page links it returns
// ErrFeedTokenUnavailable since the token cannot be obtained without auth.
// Invalid/unclassified URLs return ErrInvalidInputURL.
func (r *Resolver) Resolve(ctx context.Context, rawURL string) (domain.FeedSource, error) {
	class, err := r.Classify(rawURL)
	if err != nil {
		return domain.FeedSource{}, err
	}

	u, _ := url.Parse(rawURL) // safe: Classify already validated

	switch class {
	case domain.ClassPodcastFeed:
		matches := podcastFeedRe.FindStringSubmatch(u.Path)
		if len(matches) < 3 {
			return domain.FeedSource{}, domain.ErrInvalidInputURL
		}
		return domain.FeedSource{
			ID:    matches[1],
			Token: matches[2],
		}, nil

	case domain.ClassPageLink:
		if r.scraper == nil {
			r.log.Warn("page link resolution requires --cookie/--browser-cookies for authentication",
				domain.F("url", rawURL),
			)
			return domain.FeedSource{}, domain.ErrFeedTokenUnavailable
		}
		r.log.Info("resolving page link via HTML scraping", domain.F("url", rawURL))
		return r.scraper.ExtractFeedSource(ctx, rawURL)

	default:
		return domain.FeedSource{}, domain.ErrInvalidInputURL
	}
}
