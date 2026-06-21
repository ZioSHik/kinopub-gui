package domain

import "time"

// MediaKind distinguishes HLS playlists from progressive (direct) streams.
type MediaKind int

const (
	MediaHLS         MediaKind = iota // m3u8 playlist
	MediaProgressive                  // direct mp4 CDN stream
)

// MediaSource is a single media URL with its declared quality.
type MediaSource struct {
	Kind    MediaKind
	URL     string
	Quality string // declared quality for selection (Req 3.6)
}

// ResolvedMedia holds all tracks resolved for an episode's selected media source.
type ResolvedMedia struct {
	Source    MediaSource
	Video     VideoTrack
	Audio     []AudioTrack
	Subtitles []SubtitleTrack
	Duration  time.Duration // total media duration from ffprobe, for progress computation
}

// VideoTrack describes the video stream.
type VideoTrack struct {
	Index      int
	Resolution string // e.g. "1920x1080"
	Bandwidth  int
	BitRate    int // actual bitrate in kb/s from ffprobe (0 if unknown)
}

// AudioTrack describes a single audio stream.
type AudioTrack struct {
	Index    int
	GroupID  string // HLS GROUP-ID
	Language string // raw language tag from source
	Studio   string // dubbing studio (HLS NAME); may be empty
}

// SubtitleTrack describes a single subtitle stream.
type SubtitleTrack struct {
	Index    int
	GroupID  string
	Language string
	Source   string // source label (HLS NAME); may be empty
}

// TrackKind distinguishes track types for progress reporting.
type TrackKind int

const (
	TrackVideo    TrackKind = iota
	TrackAudio
	TrackSubtitle
)

// TrackRef identifies a specific track by kind and index.
type TrackRef struct {
	Kind  TrackKind
	Index int
}
