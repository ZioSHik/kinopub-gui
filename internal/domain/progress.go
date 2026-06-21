package domain

// SeriesPlan describes the scope of a download run for progress tracking.
type SeriesPlan struct {
	Title            string      // series title for display
	Total            int         // total episodes in the series (including already completed)
	Seasons          map[int]int // season number → total episode count in that season
	AlreadyCompleted int         // episodes already completed from previous sessions
	CompletedPerSeason map[int]int // season → already completed count from previous sessions
}

// ProgressSnapshot captures the current progress state at a point in time.
type ProgressSnapshot struct {
	SeriesPercent int              // [0,100] floor
	SeasonPercent map[int]int      // season → [0,100] floor
	Episode       *EpisodeProgress
}

// EpisodeProgress tracks the download progress of a single episode.
type EpisodeProgress struct {
	Key     EpisodeKey
	Percent int              // [0,100] floor
	Tracks  map[TrackRef]int // per-track [0,100] floor
	State   EpisodeState
}

// EpisodeState represents the lifecycle state of an episode download.
type EpisodeState int

const (
	EpisodePending   EpisodeState = iota
	EpisodeRunning
	EpisodeCompleted
	EpisodeFailed
)

// ProgressSink receives per-track progress updates from the downloader.
type ProgressSink interface {
	TrackProgress(key EpisodeKey, track TrackRef, percent int)
}

// ByteProgressSink extends ProgressSink with byte-level progress reporting.
// Implementations that support it can show download speed and file size.
type ByteProgressSink interface {
	ProgressSink
	// ByteProgress reports bytes downloaded out of total for an episode.
	// When total is not known exactly (e.g. an estimate), implementations may
	// still use it for size/ETA display; callers should mark estimates via
	// SegmentProgressSink.
	ByteProgress(key EpisodeKey, downloaded, total int64)
}

// SegmentProgressSink extends ProgressSink with HLS segment-level progress.
// Implementations that support it can show "downloaded/total segments" and an
// approximate total size derived from the average segment size so far.
type SegmentProgressSink interface {
	ProgressSink
	// SegmentProgress reports how many segments have been downloaded out of the
	// total, along with the bytes downloaded so far and an approximate total
	// size estimated from the average segment size. approxTotalBytes is 0 when
	// no estimate is available yet.
	SegmentProgress(key EpisodeKey, doneSegments, totalSegments int, downloadedBytes, approxTotalBytes int64)
}

// TrackProgressInfo describes the download progress of a single HLS track
// (the video, or one of the audio tracks) for nested display.
type TrackProgressInfo struct {
	Label            string // "Video", "Audio: Русский", etc.
	DoneSegments     int    // segments downloaded so far for this track
	TotalSegments    int    // total segments for this track
	DownloadedBytes  int64  // bytes downloaded so far for this track
	ApproxTotalBytes int64  // estimated total size of this track (0 if unknown)
}

// HLSProgressSink extends ProgressSink with detailed, per-track HLS progress so
// the UI can render nested bars for the video and each audio track.
type HLSProgressSink interface {
	ProgressSink
	// HLSProgress reports the full per-track breakdown for an episode.
	HLSProgress(key EpisodeKey, tracks []TrackProgressInfo)
}
