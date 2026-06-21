package kinopub

import (
	"context"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Dependencies holds all injectable interfaces required by the App.
type Dependencies struct {
	Logger           domain.Logger
	InputResolver    domain.InputResolver
	FeedParser       domain.FeedParser
	MediaResolver    domain.MediaResolver
	Scheduler        domain.Scheduler
	Downloader       domain.Downloader
	ProxyProvider    domain.ProxyProvider
	ProgressReporter domain.ProgressReporter
	StateStore       domain.StateStore
	OutputLayout     domain.OutputLayout

	// Optional: HLS pipeline components (nil = HLS pipeline disabled).
	HLSDownloader domain.HLSDownloader // nil when auth unavailable
	PageScraper   domain.PageScraper   // nil when auth unavailable

	// Optional: interactive audio-track picker. nil disables the menu.
	AudioChooser domain.AudioChooser
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
