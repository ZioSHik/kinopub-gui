// Package gui implements a self-contained web interface for the kinopub
// downloader. It drives the same Go engine the CLI uses (internal/app/kinopub)
// through its programmatic entry point, so the GUI gets real structured progress
// events instead of parsing terminal output. A small HTTP server exposes a REST
// API plus a Server-Sent-Events stream and serves an embedded React frontend.
package gui

import (
	"context"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/app/kinopub"
	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/httpx"
	"github.com/niazlv/kinopub-downloader/internal/services/downloader"
	"github.com/niazlv/kinopub-downloader/internal/services/feedparser"
	"github.com/niazlv/kinopub-downloader/internal/services/hlsdownloader"
	"github.com/niazlv/kinopub-downloader/internal/services/inputresolver"
	"github.com/niazlv/kinopub-downloader/internal/services/mediaresolver"
	"github.com/niazlv/kinopub-downloader/internal/services/outputlayout"
	"github.com/niazlv/kinopub-downloader/internal/services/pagescraper"
	"github.com/niazlv/kinopub-downloader/internal/services/proxyprovider"
	"github.com/niazlv/kinopub-downloader/internal/services/scheduler"
	"github.com/niazlv/kinopub-downloader/internal/services/statestore"
)

// buildEngineDeps constructs the full set of engine dependencies for one run,
// wiring the GUI-supplied logger, progress reporter and (optional) audio chooser
// in place of the CLI's terminal implementations. It mirrors the wiring done by
// the CLI entrypoint so the GUI reaches feature parity with the command line.
func buildEngineDeps(
	cfg domain.RunConfig,
	logger domain.Logger,
	reporter domain.ProgressReporter,
	chooser domain.AudioChooser,
) (kinopub.Dependencies, error) {
	// Proxy provider.
	proxyProv, err := proxyprovider.New(cfg.ProxyURL)
	if err != nil {
		return kinopub.Dependencies{}, err
	}

	// Auth-aware HTTP client: every request carries the configured
	// Cookie / User-Agent / extra headers, plus the Referer the CDN requires.
	auth := domain.RequestAuth{
		Cookie:    cfg.Cookie,
		UserAgent: cfg.UserAgent,
		Headers:   cfg.Headers,
	}
	if auth.Headers == nil {
		auth.Headers = make(map[string]string)
	}
	if auth.Headers["Referer"] == "" {
		auth.Headers["Referer"] = "https://kino.pub/"
	}
	httpClient := httpx.WithAuth(proxyProv.HTTPClient(), auth)

	// Input resolver — with page scraper when auth is available.
	var resolverOpts []inputresolver.Option
	if !auth.IsZero() {
		scraper := pagescraper.New(httpClient, logger)
		resolverOpts = append(resolverOpts, inputresolver.WithPageScraper(scraper))
	}
	inputRes := inputresolver.New(logger, resolverOpts...)

	feedPars := feedparser.New(httpClient, logger)

	mediaRes := mediaresolver.New(httpClient, makeRunOutput(), logger, auth)

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
		InputResolver:    inputRes,
		FeedParser:       feedPars,
		MediaResolver:    mediaRes,
		Scheduler:        sched,
		Downloader:       dl,
		ProxyProvider:    proxyProv,
		ProgressReporter: reporter,
		StateStore:       stateStr,
		OutputLayout:     layout,
	}

	// Optional HLS pipeline: only when auth is present (page scraping needs
	// cookies to reach the player page).
	if !auth.IsZero() {
		scraper := pagescraper.New(httpClient, logger)
		hlsDl := hlsdownloader.New(httpClient, auth, logger,
			hlsdownloader.WithConcurrency(cfg.MaxConcurrency),
			hlsdownloader.WithProxy(proxyProv.ProxyURL()))
		deps.PageScraper = scraper
		deps.HLSDownloader = hlsDl
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

// makeRunOutput executes a command and captures stdout; on failure stderr is
// folded into the error for diagnostics.
func makeRunOutput() mediaresolver.RunOutputFunc {
	return func(ctx context.Context, name string, args, env []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return nil, &runError{err: err, stderr: msg}
			}
			return nil, err
		}
		return out, nil
	}
}

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

// runError carries a command failure together with its stderr text.
type runError struct {
	err    error
	stderr string
}

func (e *runError) Error() string { return e.err.Error() + ": " + e.stderr }
func (e *runError) Unwrap() error { return e.err }
