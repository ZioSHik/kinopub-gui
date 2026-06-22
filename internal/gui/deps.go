// Package gui implements a self-contained web interface for the kinopub
// downloader. It drives the same Go engine the CLI used (internal/app/kinopub)
// through its programmatic entry point, so the GUI gets real structured progress
// events. Downloads resolve exclusively through the official kino.pub JSON API.
package gui

import (
	"context"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/app/kinopub"
	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/httpx"
	"github.com/ZioSHik/kinopub-gui/internal/services/downloader"
	"github.com/ZioSHik/kinopub-gui/internal/services/hlsdownloader"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
	"github.com/ZioSHik/kinopub-gui/internal/services/outputlayout"
	"github.com/ZioSHik/kinopub-gui/internal/services/proxyprovider"
	"github.com/ZioSHik/kinopub-gui/internal/services/scheduler"
	"github.com/ZioSHik/kinopub-gui/internal/services/statestore"
)

// buildEngineDeps constructs the engine dependencies for one run. The source is
// always the official kino.pub JSON API: an API-backed PageScraper resolves the
// item, and the HLS downloader streams it. Cookie/RSS sources were removed.
func buildEngineDeps(
	cfg domain.RunConfig,
	apiClient *kinopubapi.Client,
	logger domain.Logger,
	reporter domain.ProgressReporter,
	chooser domain.AudioChooser,
) (kinopub.Dependencies, error) {
	proxyProv, err := proxyprovider.New(cfg.ProxyURL)
	if err != nil {
		return kinopub.Dependencies{}, err
	}

	// The CDN only requires a Referer/User-Agent; hls4 URLs are token-signed, so
	// no cookie is needed for the stream itself.
	auth := domain.RequestAuth{
		UserAgent: cfg.UserAgent,
		Headers:   map[string]string{"Referer": "https://kino.pub/"},
	}
	httpClient := httpx.WithAuth(proxyProv.HTTPClient(), auth)

	layout := outputlayout.New(cfg.Container)

	outputDir := cfg.OutputPath
	if outputDir == "" {
		outputDir, _ = os.Getwd()
	}
	stateStr := statestore.New(outputDir, logger)

	dl := downloader.New(
		makeRunFunc(),
		proxyProv,
		logger,
		downloader.WithFFmpegPath(cfg.FFmpegPath),
		downloader.WithAuth(auth),
		downloader.WithExtraArgs(cfg.FFmpegExtraArgs),
		downloader.WithNoChunked(cfg.NoChunked),
		downloader.WithHTTPClient(httpClient),
	)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	sched := scheduler.New(
		scheduler.Config{
			MaxConcurrency: cfg.MaxConcurrency,
			MaxRetries:     cfg.MaxRetries,
			MinIntervalMS:  cfg.MinIntervalMS,
			GracePeriod:    cfg.GracePeriod,
		},
		realClock{},
		logger,
		rng,
	)

	deps := kinopub.Dependencies{
		Logger:           logger,
		Scheduler:        sched,
		Downloader:       dl,
		ProxyProvider:    proxyProv,
		ProgressReporter: reporter,
		StateStore:       stateStr,
		OutputLayout:     layout,
		PageScraper:      kinopubapi.NewScraper(apiClient, logger),
		// Segment concurrency is kept at the downloader's modest default (a few
		// segments per episode); cfg.MaxConcurrency now governs how many
		// episodes download in parallel (see engine.runHLS), so the two budgets
		// don't multiply into a CDN flood.
		HLSDownloader: hlsdownloader.New(httpClient, auth, logger,
			hlsdownloader.WithProxy(proxyProv.ProxyURL())),
	}

	if chooser != nil {
		deps.AudioChooser = chooser
	}

	return deps, nil
}

// realClock implements domain.Clock using the real system clock.
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) Sleep(d time.Duration)                  { time.Sleep(d) }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// makeRunFunc executes a command, streaming stdout to the provided writer.
// ffmpeg stderr is discarded — progress arrives via -progress pipe:1 on stdout.
func makeRunFunc() downloader.RunFunc {
	return func(ctx context.Context, name string, args, env []string, stdout io.Writer) error {
		cmd := exec.CommandContext(ctx, name, args...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		cmd.Stdout = stdout
		cmd.Stderr = io.Discard
		return cmd.Run()
	}
}
