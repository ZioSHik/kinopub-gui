package kinopub

import (
	"context"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// Dependencies holds all injectable interfaces required by the App. Downloads
// resolve via the official kino.pub API + HLS pipeline (the cookie/RSS sources
// were removed).
type Dependencies struct {
	Logger           domain.Logger
	Scheduler        domain.Scheduler
	Downloader       domain.Downloader
	ProxyProvider    domain.ProxyProvider
	ProgressReporter domain.ProgressReporter
	StateStore       domain.StateStore
	OutputLayout     domain.OutputLayout

	// HLS pipeline: an API-backed PageScraper resolves the item, HLSDownloader
	// streams it. Both required for a download (validated below).
	HLSDownloader domain.HLSDownloader
	PageScraper   domain.PageScraper

	// Optional: interactive audio-track picker. nil disables the menu.
	AudioChooser domain.AudioChooser

	// Optional: PrioritizeRequests delivers episode keys that should jump to the
	// front of this run's download queue. The engine drains it between episodes,
	// so a user can promote a not-yet-started episode without waiting. nil
	// disables live reordering.
	PrioritizeRequests <-chan domain.EpisodeKey

	// Optional: PauseRequests / ResumeRequests hold or release an individual
	// episode — including one that is actively downloading (its download is
	// canceled and its partial segments preserved). A paused episode is set aside
	// and skipped; the run stays alive while any episode is held, so it can be
	// resumed. nil disables per-episode pause.
	PauseRequests  <-chan domain.EpisodeKey
	ResumeRequests <-chan domain.EpisodeKey

	// Optional: RetryRequests re-queues an episode that already failed in THIS run
	// (re-attempting it in place, without a new job). nil disables live retry.
	RetryRequests <-chan domain.EpisodeKey

	// Optional: Paused reports whether the whole run is being paused (as opposed
	// to canceled). When it returns true on exit, partial segment data (.hls-tmp)
	// is preserved so a later resume continues instead of restarting. nil ⇒ never
	// paused (a context cancel is a hard stop that cleans up).
	Paused func() bool
}

// App is the composition root that wires all services together and exposes
// the download engine (Req 16.1, 16.2).
type App struct {
	deps Dependencies
}

// New constructs an App after validating that all dependencies are provided.
// Returns ErrMissingDependency (wrapping the field name) if any dependency is nil (Req 16.5).
func New(deps Dependencies) (*App, error) {
	if err := validateDependencies(deps); err != nil {
		return nil, err
	}
	return &App{deps: deps}, nil
}

// Run implements domain.DownloadEngine. It orchestrates the full download
// workflow using only the injected interfaces (Req 16.3, 16.4).
func (a *App) Run(ctx context.Context, cfg domain.RunConfig) (domain.RunResult, error) {
	eng := &engine{deps: a.deps}
	return eng.run(ctx, cfg)
}
