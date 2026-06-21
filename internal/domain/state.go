package domain

import "time"

// DownloadState persists which episodes have been completed for a series,
// along with series-level metadata for recovery/provenance.
type DownloadState struct {
	Series   SeriesID                `json:"series"`
	Metadata *SeriesMetadata         `json:"metadata,omitempty"`
	Completed map[string]CompletedRec `json:"completed"` // key = "S{n}E{n}"
}

// SeriesMetadata stores provenance and descriptive information about the series.
type SeriesMetadata struct {
	Title         string `json:"title,omitempty"`
	OriginalTitle string `json:"original_title,omitempty"`
	Description   string `json:"description,omitempty"`
	PosterURL     string `json:"poster_url,omitempty"`
	FeedURL       string `json:"feed_url,omitempty"`
	InputURL      string `json:"input_url,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CompletedRec records metadata about a completed episode download.
type CompletedRec struct {
	Season      int       `json:"season"`
	Episode     int       `json:"episode"`
	Path        string    `json:"path"`
	Bytes       int64     `json:"bytes"`
	CompletedAt time.Time `json:"completed_at"`

	// Episode metadata for recovery.
	Title      string `json:"title,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Resolution string `json:"resolution,omitempty"` // actual resolution from ffprobe, e.g. "1920x1072"
	BitRate    int    `json:"bitrate_kbps,omitempty"` // video bitrate in kb/s from ffprobe
	PageLink   string `json:"page_link,omitempty"`
	MediaURL   string `json:"media_url,omitempty"`
}

// CompletedInfo carries all information needed to record a completed download.
type CompletedInfo struct {
	Key        EpisodeKey
	Path       string
	Bytes      int64
	Title      string
	Quality    string
	Resolution string // actual resolution from ffprobe
	BitRate    int    // video bitrate in kb/s from ffprobe
	PageLink   string
	MediaURL   string
}
