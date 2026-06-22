// Package kinopubapi is a client for the official kino.pub JSON API
// (https://api.service-kp.com — the same API the mobile/TV apps use). It
// provides device-code OAuth authentication and the catalog/discovery endpoints
// (search, tops, collections, item details with озвучки) plus an adapter that
// implements domain.PageScraper so an API-sourced item flows into the existing
// HLS download pipeline unchanged.
package kinopubapi

import "encoding/json"

// The well-known client credentials embedded in third-party kino.pub clients
// (xbmc/forkplayer/etc.). The device-code flow authenticates the user's own
// paid account, exactly like signing a TV app into the account.
const (
	DefaultClientID     = "xbmc"
	DefaultClientSecret = "cgg3gtifu46urtfp2zp1nqtba0k2ezxh"
	apiHost             = "https://api.service-kp.com"
)

// ---------------------------------------------------------------------------
// OAuth device-code DTOs
// ---------------------------------------------------------------------------

// deviceCodeResp is the response to a grant_type=device_code request.
type deviceCodeResp struct {
	Code            string `json:"code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`

	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// tokenResp is the response to a grant_type=device_token / refresh_token request.
type tokenResp struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`

	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ---------------------------------------------------------------------------
// Catalog DTOs (subset of the kino.pub item schema that the UI needs)
// ---------------------------------------------------------------------------

// NamedID is the common {id,title} shape used by genres, countries, audio type
// and author, etc.
type NamedID struct {
	ID    json.Number `json:"id"`
	Title string      `json:"title"`
}

// Posters holds the poster image URLs at different sizes.
type Posters struct {
	Small  string `json:"small"`
	Medium string `json:"medium"`
	Big    string `json:"big"`
	Wide   string `json:"wide"`
}

// Best returns the most suitable poster URL, preferring larger sizes.
func (p Posters) Best() string {
	switch {
	case p.Big != "":
		return p.Big
	case p.Medium != "":
		return p.Medium
	case p.Wide != "":
		return p.Wide
	default:
		return p.Small
	}
}

// Item is a catalog entry. The same struct serves both list/search summaries
// and the full single-item response (fields absent in summaries stay zero).
type Item struct {
	ID            json.Number `json:"id"`
	Type          string      `json:"type"`
	Title         string      `json:"title"`
	Subname       string      `json:"subname"` // original title, when provided separately
	Year          int         `json:"year"`
	Plot          string      `json:"plot"`
	Director      string      `json:"director"` // comma-separated names
	Cast          string      `json:"cast"`     // comma-separated names
	Posters       Posters     `json:"posters"`
	IMDBRating    float64     `json:"imdb_rating"`
	KinopoiskRate float64     `json:"kinopoisk_rating"`
	RatingVotes   int         `json:"rating"`            // kino.pub upvote count (NOT a score)
	RatingPercent int         `json:"rating_percentage"` // kino.pub liked %, → local 0–10 score
	Quality       int         `json:"quality"`           // max resolution (e.g. 2160, 1080)
	Genres        []NamedID   `json:"genres"`
	Countries     []NamedID   `json:"countries"`
	Duration      Duration    `json:"duration"`

	// Movies expose their playable versions under videos; serials under seasons.
	Videos  []Video  `json:"videos"`
	Seasons []Season `json:"seasons"`

	// Watching progress (populated by /v1/watching/*; absent in the catalog).
	Total   int `json:"total"`
	Watched int `json:"watched"`
	New     int `json:"new"`

	// Note is not from the item JSON; Watching() sets it to a progress label.
	// WatchedAt is the history last_seen unix time; HistSeason/HistEpisode are
	// the last-watched S/E from history (all 0 outside their context).
	Note        string `json:"-"`
	WatchedAt   int64  `json:"-"`
	HistSeason  int    `json:"-"`
	HistEpisode int    `json:"-"`
}

// Duration carries the average/total runtime in seconds. The API may report
// these as fractional numbers, so they are floats.
type Duration struct {
	Average float64 `json:"average"`
	Total   float64 `json:"total"`
}

// Season groups episodes for a serial.
type Season struct {
	ID       json.Number `json:"id"`
	Number   int         `json:"number"`
	Title    string      `json:"title"`
	Episodes []Episode   `json:"episodes"`
}

// Episode is one playable serial episode. Duration is seconds (may be
// fractional, hence float).
type Episode struct {
	ID        json.Number `json:"id"`
	Number    int         `json:"number"`
	Title     string      `json:"title"`
	Duration  float64     `json:"duration"`
	Thumbnail string      `json:"thumbnail"`
	Watched   int         `json:"watched"` // 1 = already watched
	Files     []File      `json:"files"`
	Audios    []Audio     `json:"audios"`
	Subtitles []Subtitle  `json:"subtitles"`
}

// Video is one playable movie version (movies have one or a few).
type Video struct {
	ID        json.Number `json:"id"`
	Number    int         `json:"number"`
	Title     string      `json:"title"`
	Duration  float64     `json:"duration"`
	Files     []File      `json:"files"`
	Audios    []Audio     `json:"audios"`
	Subtitles []Subtitle  `json:"subtitles"`
}

// File is one quality variant with its stream URLs.
type File struct {
	Quality string  `json:"quality"` // "1080p", "720p", ...
	Codec   string  `json:"codec"`
	W       int     `json:"w"`
	H       int     `json:"h"`
	URL     FileURL `json:"url"`
}

// FileURL holds the per-protocol stream URLs. hls4 is the adaptive HLS master
// playlist the existing downloader consumes.
type FileURL struct {
	HTTP string `json:"http"`
	HLS  string `json:"hls"`
	HLS2 string `json:"hls2"`
	HLS4 string `json:"hls4"`
}

// Manifest returns the best HLS manifest URL, preferring hls4 (fMP4) which is
// what kino.pub's own player and the existing HLS pipeline expect.
func (u FileURL) Manifest() string {
	switch {
	case u.HLS4 != "":
		return u.HLS4
	case u.HLS2 != "":
		return u.HLS2
	default:
		return u.HLS
	}
}

// Audio describes one voiceover track (озвучка).
type Audio struct {
	ID       json.Number `json:"id"`
	Index    int         `json:"index"`
	Codec    string      `json:"codec"`
	Lang     string      `json:"lang"`
	Channels int         `json:"channels"`
	Type     NamedID     `json:"type"`   // дубляж / многоголосый / ...
	Author   NamedID     `json:"author"` // LostFilm / HDrezka / ...
}

// Subtitle describes one subtitle track. Only the fields the UI needs are
// declared; the JSON decoder ignores the rest (avoids type-mismatch surprises
// on fields whose API types vary).
type Subtitle struct {
	Lang string `json:"lang"`
	URL  string `json:"url"`
}

// Collection is a curated подборка.
type Collection struct {
	ID      json.Number `json:"id"`
	Title   string      `json:"title"`
	Posters Posters     `json:"posters"`
}

// BookmarkFolder is one of the account's bookmark folders (закладки). Count is
// the number of items the folder holds.
type BookmarkFolder struct {
	ID    json.Number `json:"id"`
	Title string      `json:"title"`
	Count int         `json:"count"`
}
