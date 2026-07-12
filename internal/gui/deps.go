// Package gui implements a self-contained web interface for the kinopub
// downloader. It drives the same Go engine the CLI used (internal/app/kinopub)
// through its programmatic entry point, so the GUI gets real structured progress
// events. Downloads resolve exclusively through the official kino.pub JSON API.
package gui

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	prioritize <-chan domain.EpisodeKey,
	pause <-chan domain.EpisodeKey,
	resume <-chan domain.EpisodeKey,
	retry <-chan domain.EpisodeKey,
	cancel <-chan domain.EpisodeKey,
	paused func() bool,
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
	deps.PrioritizeRequests = prioritize
	deps.PauseRequests = pause
	deps.ResumeRequests = resume
	deps.RetryRequests = retry
	deps.CancelRequests = cancel
	deps.Paused = paused

	return deps, nil
}

// realClock implements domain.Clock using the real system clock.
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) Sleep(d time.Duration)                  { time.Sleep(d) }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// makeRunFunc executes a command, streaming stdout to the provided writer.
// Progress arrives via -progress pipe:1 on stdout; ffmpeg writes the actual
// error message to stderr, so we keep its tail and fold it into the returned
// error on failure — otherwise a remux failure surfaces as a bare, unactionable
// "exit status N" with no clue why.
func makeRunFunc() downloader.RunFunc {
	return func(ctx context.Context, name string, args, env []string, stdout io.Writer) error {
		cmd := exec.CommandContext(ctx, name, args...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		cmd.Stdout = stdout
		tail := &tailWriter{max: 8192}
		cmd.Stderr = tail
		err := cmd.Run()
		if err != nil {
			if msg := tail.lastLines(8); msg != "" {
				return fmt.Errorf("%w: %s", err, msg)
			}
		}
		return err
	}
}

// tailWriter keeps only the last max bytes written to it, so a verbose ffmpeg
// stderr can be summarized on failure without buffering the whole stream. It is
// safe for concurrent writes from the command's stderr pump.
type tailWriter struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	return len(p), nil
}

// lastLines returns the last n non-empty lines of captured output joined with
// " | " — enough to convey the real ffmpeg error without dumping the full log.
func (w *tailWriter) lastLines(n int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines := strings.Split(strings.ReplaceAll(string(w.buf), "\r", "\n"), "\n")
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			out = append([]string{s}, out...)
		}
	}
	return strings.Join(out, " | ")
}
