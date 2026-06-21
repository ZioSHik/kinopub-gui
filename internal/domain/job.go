package domain

// Job represents a single download task for one episode.
type Job struct {
	Episode    Episode
	Media      ResolvedMedia
	OutPath    string
	PosterPath string // path to a local poster image file to embed as cover art (optional)
	SeriesTitle string // series title for container metadata (optional)
}

// JobOutcome records the result of a single job after all attempts.
type JobOutcome struct {
	Key       EpisodeKey
	Succeeded bool
	Err       error
	Attempts  int
}

// RunResult summarizes the outcome of a complete download run.
type RunResult struct {
	Total     int
	Succeeded int
	Failed    int
	Skipped   int
	Outcomes  []JobOutcome
}

// RunSummary is the scheduler's view of the run outcome.
type RunSummary struct {
	Total     int
	Succeeded int
	Failed    int
	Outcomes  []JobOutcome
}

// ErrorClass classifies a failed attempt for retry decisions (Req 5).
type ErrorClass int

const (
	ClassNonRetryable ErrorClass = iota // 401, 404
	ClassRetryable                      // 403, 429, 5xx, conn timeout/reset, DNS
	ClassAuth                           // 401/403 surfaced as auth (Req 17)
)
