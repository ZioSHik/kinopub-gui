package domain

import "time"

// SeriesID is a stable identifier derived from the feed (e.g., podcast numeric id).
type SeriesID string

// Series represents a complete series catalog parsed from a feed.
type Series struct {
	ID            SeriesID
	Title         string
	OriginalTitle string
	Description   string
	PosterURL     string
	Seasons       []Season // ascending by Number (Req 2.4)
}

// Season groups episodes by season number.
type Season struct {
	Number   int
	Episodes []Episode // ascending by Number (Req 2.4)
}

// Episode represents a single downloadable episode.
type Episode struct {
	Key          EpisodeKey
	Title        string
	Quality      string        // declared quality, e.g. "1080p"
	PageLink     string
	Duration     time.Duration // media duration for progress computation
	MediaSources []MediaSource // at least one (Req 2.3)
}

// EpisodeKey uniquely identifies an episode within a series.
type EpisodeKey struct {
	Series  SeriesID
	Season  int
	Episode int
}
