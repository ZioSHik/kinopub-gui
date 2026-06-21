package progress

import (
	"sync"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// LogReporter is a non-interactive fallback that emits discrete log records
// on episode start/complete/fail, each including the updated series percentage
// and that episode's season percentage. (Req 10.6, 10.8)
type LogReporter struct {
	logger domain.Logger

	mu              sync.Mutex
	plan            domain.SeriesPlan
	completedTotal  int
	completedSeason map[int]int
}

// NewLog creates a LogReporter that emits progress as log records.
func NewLog(logger domain.Logger) *LogReporter {
	return &LogReporter{
		logger: logger,
	}
}

// Start begins reporting for the full series plan.
func (r *LogReporter) Start(plan domain.SeriesPlan) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.plan = plan
	r.completedTotal = plan.AlreadyCompleted
	r.completedSeason = make(map[int]int, len(plan.Seasons))
	for season, count := range plan.CompletedPerSeason {
		r.completedSeason[season] = count
	}

	r.logger.Info("progress reporting started",
		domain.F("total_episodes", plan.Total),
		domain.F("already_completed", plan.AlreadyCompleted),
		domain.F("seasons", len(plan.Seasons)),
	)
}

// EpisodeStarted signals that an episode download has begun.
func (r *LogReporter) EpisodeStarted(key domain.EpisodeKey) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("episode started",
		domain.F("season", key.Season),
		domain.F("episode", key.Episode),
		domain.F("series_percent", r.seriesPercent()),
		domain.F("season_percent", r.seasonPercent(key.Season)),
	)
}

// TrackProgress is a no-op for the log reporter; per-track updates are not
// emitted as individual log records to avoid excessive noise.
func (r *LogReporter) TrackProgress(_ domain.EpisodeKey, _ domain.TrackRef, _ int) {
	// Non-interactive mode does not emit per-track progress.
}

// EpisodeCompleted signals that an episode download finished successfully.
func (r *LogReporter) EpisodeCompleted(key domain.EpisodeKey) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.completedTotal++
	r.completedSeason[key.Season]++

	r.logger.Info("episode completed",
		domain.F("season", key.Season),
		domain.F("episode", key.Episode),
		domain.F("series_percent", r.seriesPercent()),
		domain.F("season_percent", r.seasonPercent(key.Season)),
	)
}

// EpisodeFailed signals that an episode download failed.
func (r *LogReporter) EpisodeFailed(key domain.EpisodeKey, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Error("episode failed",
		domain.F("season", key.Season),
		domain.F("episode", key.Episode),
		domain.F("error", err.Error()),
		domain.F("series_percent", r.seriesPercent()),
		domain.F("season_percent", r.seasonPercent(key.Season)),
	)
}

// EpisodeDeferred signals that an episode failed on a transient error and was
// parked for a later retry.
func (r *LogReporter) EpisodeDeferred(key domain.EpisodeKey, err error, attempts int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Warn("episode deferred for retry",
		domain.F("season", key.Season),
		domain.F("episode", key.Episode),
		domain.F("attempts", attempts),
		domain.F("error", err.Error()),
	)
}

// Stop is a no-op for the log reporter; there is no live display to tear down.
func (r *LogReporter) Stop() {
	r.logger.Info("progress reporting stopped")
}

// seriesPercent computes the overall series completion percentage.
// Must be called with r.mu held.
func (r *LogReporter) seriesPercent() int {
	return computePercent(r.completedTotal, r.plan.Total)
}

// seasonPercent computes the season completion percentage.
// Must be called with r.mu held.
func (r *LogReporter) seasonPercent(season int) int {
	total := r.plan.Seasons[season]
	completed := r.completedSeason[season]
	return computePercent(completed, total)
}

// Verify that *LogReporter satisfies domain.ProgressReporter at compile time.
var _ domain.ProgressReporter = (*LogReporter)(nil)
