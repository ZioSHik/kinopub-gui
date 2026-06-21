// Package downloader implements domain.Downloader — it orchestrates ffmpeg
// invocations to download and mux media for a single episode (Req 7, 8, 9).
// For progressive MP4 sources, it supports a chunked HTTP download mode with
// resume capability, falling back to direct ffmpeg streaming on failure.
package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Compile-time interface assertions.
var (
	_ domain.Downloader  = (*Downloader)(nil)
	_ domain.JobExecutor = (*Downloader)(nil)
	_ domain.HLSMuxer    = (*Downloader)(nil)
)

// RunFunc is a function that runs a command, streaming stdout to the provided
// writer. It blocks until the command completes. The writer receives the
// command's stdout (used for -progress pipe:1 output).
type RunFunc func(ctx context.Context, name string, args, env []string, stdout io.Writer) error

// DownloadMode indicates which download strategy was used.
type DownloadMode string

const (
	ModeChunked DownloadMode = "chunked" // HTTP Range-based with resume
	ModeDirect  DownloadMode = "direct"  // ffmpeg stream copy from URL
)

// Downloader implements domain.Downloader and domain.JobExecutor.
type Downloader struct {
	run        RunFunc
	proxy      domain.ProxyProvider
	logger     domain.Logger
	ffmpegPath string
	auth       domain.RequestAuth
	extraArgs  []string
	noChunked  bool
	httpClient *http.Client
}

// Option configures the Downloader.
type Option func(*Downloader)

// WithFFmpegPath sets a custom ffmpeg binary path.
func WithFFmpegPath(path string) Option {
	return func(d *Downloader) {
		d.ffmpegPath = path
	}
}

// WithAuth sets the request authentication (Cookie, User-Agent, extra headers)
// propagated to ffmpeg so its requests pass Cloudflare and kino.pub auth.
func WithAuth(auth domain.RequestAuth) Option {
	return func(d *Downloader) {
		d.auth = auth
	}
}

// WithExtraArgs sets additional ffmpeg arguments injected before the output
// path. This allows users to override encoding settings (e.g. -c:v libx265)
// or add filters on the fly.
func WithExtraArgs(args []string) Option {
	return func(d *Downloader) {
		d.extraArgs = args
	}
}

// WithNoChunked disables the chunked download mode, forcing all downloads
// through ffmpeg directly.
func WithNoChunked(noChunked bool) Option {
	return func(d *Downloader) {
		d.noChunked = noChunked
	}
}

// WithHTTPClient sets the HTTP client used for chunked downloads.
func WithHTTPClient(client *http.Client) Option {
	return func(d *Downloader) {
		d.httpClient = client
	}
}

// New creates a new Downloader.
//   - run: function to execute ffmpeg, streaming stdout to a writer
//   - proxy: provides proxy environment for ffmpeg
//   - logger: structured logger
func New(run RunFunc, proxy domain.ProxyProvider, logger domain.Logger, opts ...Option) *Downloader {
	d := &Downloader{
		run:        run,
		proxy:      proxy,
		logger:     logger.Component("downloader"),
		ffmpegPath: "ffmpeg",
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Download runs the download for one episode. For progressive MP4 sources
// (when chunked mode is enabled), it first downloads the raw file via HTTP
// Range requests with resume capability, then remuxes with ffmpeg to add
// metadata and labels. For HLS sources or when chunked is disabled, it uses
// the traditional ffmpeg-based streaming approach.
func (d *Downloader) Download(ctx context.Context, job domain.Job, sink domain.ProgressSink) error {
	// Determine if we can use chunked mode.
	// Determine if we can use chunked mode.
	// Skip chunked for local files (no http:// prefix) — they're already on disk.
	isRemoteURL := strings.HasPrefix(job.Media.Source.URL, "http://") ||
		strings.HasPrefix(job.Media.Source.URL, "https://")

	useChunked := !d.noChunked &&
		d.httpClient != nil &&
		job.Media.Source.Kind == domain.MediaProgressive &&
		isRemoteURL &&
		len(d.extraArgs) == 0 // extra ffmpeg args imply transcoding, skip chunked

	if useChunked {
		err := d.downloadChunked(ctx, job, sink)
		if err == nil {
			return nil
		}
		// Chunked failed — fall back to direct ffmpeg.
		d.logger.Warn("chunked download failed, falling back to direct ffmpeg",
			domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
			domain.F("error", err.Error()),
		)
	}

	return d.downloadDirect(ctx, job, sink)
}

// downloadChunked implements the chunked HTTP Range download + ffmpeg remux.
func (d *Downloader) downloadChunked(ctx context.Context, job domain.Job, sink domain.ProgressSink) error {
	d.logger.Info("starting chunked download",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
		domain.F("mode", string(ModeChunked)),
		domain.F("output", job.OutPath),
	)

	// 1. Download raw file via chunked HTTP.
	rawPath := job.OutPath + ".raw"
	chunked := NewChunked(d.httpClient, d.auth, d.logger)

	if err := chunked.Download(ctx, job.Media.Source.URL, rawPath, job.Episode.Key, sink); err != nil {
		return fmt.Errorf("chunked download: %w", err)
	}

	// 2. Remux with ffmpeg: local file → final container with metadata.
	d.logger.Info("remuxing downloaded file",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
	)

	if err := d.remuxLocal(ctx, job, rawPath); err != nil {
		// Clean up raw file on remux failure.
		os.Remove(rawPath)
		return fmt.Errorf("remux: %w", err)
	}

	// 3. Clean up raw file after successful remux.
	os.Remove(rawPath)

	d.logger.Info("download completed",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
		domain.F("mode", string(ModeChunked)),
		domain.F("output", job.OutPath),
	)

	return nil
}

// remuxLocal runs ffmpeg to remux a local raw file into the final container
// with all metadata, poster, and audio/subtitle labels.
func (d *Downloader) remuxLocal(ctx context.Context, job domain.Job, rawPath string) error {
	return d.RemuxLocal(ctx, job, rawPath)
}

// MuxHLS combines a downloaded HLS video file with separate audio tracks into
// the final container at job.OutPath. For demuxed HLS, video and audio come
// from separate files; this maps them all together with -c copy and applies
// per-track labels/languages.
func (d *Downloader) MuxHLS(ctx context.Context, job domain.Job, hls *domain.HLSDownloadResult) error {
	d.logger.Info("muxing HLS streams",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
		domain.F("audio_tracks", len(hls.AudioTracks)),
		domain.F("output", job.OutPath),
	)

	tempPath := job.OutPath + ".tmp"
	args := BuildHLSMuxArgs(job, hls, tempPath)

	runErr := d.run(ctx, d.ffmpegPath, args, nil, nil)
	if runErr != nil {
		os.Remove(tempPath)
		return fmt.Errorf("%w: %v", domain.ErrFFmpegFailed, runErr)
	}

	info, err := os.Stat(tempPath)
	if err != nil || info.Size() == 0 {
		os.Remove(tempPath)
		return domain.ErrEmptyOutput
	}

	if err := os.Rename(tempPath, job.OutPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename temp to final: %w", err)
	}

	return nil
}

// RemuxLocal remuxes a local media file (e.g. a concatenated HLS .ts) into the
// final container at job.OutPath. It copies ALL streams (video + every audio
// track + subtitles) using -map 0, applies container metadata and poster, and
// does NOT inject any HTTP auth options (the input is a local file).
func (d *Downloader) RemuxLocal(ctx context.Context, job domain.Job, localPath string) error {
	d.logger.Info("remuxing local file",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
		domain.F("input", localPath),
		domain.F("output", job.OutPath),
	)

	tempPath := job.OutPath + ".tmp"
	args := BuildRemuxArgs(job, localPath, tempPath)

	// Run ffmpeg (no proxy env, no auth — local file).
	runErr := d.run(ctx, d.ffmpegPath, args, nil, nil)
	if runErr != nil {
		os.Remove(tempPath)
		return fmt.Errorf("%w: %v", domain.ErrFFmpegFailed, runErr)
	}

	info, err := os.Stat(tempPath)
	if err != nil || info.Size() == 0 {
		os.Remove(tempPath)
		return domain.ErrEmptyOutput
	}

	if err := os.Rename(tempPath, job.OutPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename temp to final: %w", err)
	}

	return nil
}

// downloadDirect is the traditional ffmpeg-based download (stream from URL).
func (d *Downloader) downloadDirect(ctx context.Context, job domain.Job, sink domain.ProgressSink) error {
	d.logger.Info("starting direct download",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
		domain.F("mode", string(ModeDirect)),
		domain.F("output", job.OutPath),
	)

	// 1. Get proxy env for ffmpeg.
	proxyEnv, err := d.proxy.FFmpegEnv()
	if err != nil {
		return fmt.Errorf("proxy env: %w", err)
	}

	// 2. Compute temp path.
	tempPath := job.OutPath + ".tmp"

	// 3. Build ffmpeg args.
	args := BuildFFmpegArgs(job, proxyEnv, d.auth, tempPath, d.extraArgs)

	// 4. Set up progress parsing.
	duration := estimateDuration(job)

	var stdout io.Writer
	var parser *progressParser

	if sink != nil && duration > 0 {
		track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}
		parser = newProgressParser(sink, job.Episode.Key, track, duration)
		stdout = parser
	}

	// 5. Run ffmpeg.
	d.logger.Debug("running ffmpeg",
		domain.F("args_count", len(args)),
		domain.F("proxy_env_count", len(proxyEnv)),
	)

	runErr := d.run(ctx, d.ffmpegPath, args, proxyEnv, stdout)

	// Close the progress parser to flush remaining data.
	if parser != nil {
		_ = parser.Close()
	}

	// 6. Handle failure.
	if runErr != nil {
		d.logger.Error("ffmpeg failed",
			domain.F("error", runErr.Error()),
			domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
		)
		_ = os.Remove(tempPath)
		return fmt.Errorf("%w: %v", domain.ErrFFmpegFailed, runErr)
	}

	// 7. Verify temp file exists and size > 0.
	info, err := os.Stat(tempPath)
	if err != nil || info.Size() == 0 {
		d.logger.Error("output file missing or empty",
			domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
			domain.F("temp_path", tempPath),
		)
		_ = os.Remove(tempPath)
		return domain.ErrEmptyOutput
	}

	// 7b. Verify duration: if we know the expected duration, check that the
	// downloaded file is at least 85% of it.
	if parser != nil && duration > 0 {
		lastPct := parser.lastPercent()
		if lastPct < 85 {
			d.logger.Error("download appears truncated",
				domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
				domain.F("last_progress_percent", lastPct),
				domain.F("expected_duration", duration.String()),
				domain.F("file_size", info.Size()),
			)
			_ = os.Remove(tempPath)
			return fmt.Errorf("%w: download truncated at %d%% (CDN may have dropped the connection)", domain.ErrFFmpegFailed, lastPct)
		}
	}

	// 8. Atomic rename to final path.
	if err := os.Rename(tempPath, job.OutPath); err != nil {
		d.logger.Error("rename failed",
			domain.F("error", err.Error()),
			domain.F("from", tempPath),
			domain.F("to", job.OutPath),
		)
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename temp to final: %w", err)
	}

	d.logger.Info("download completed",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", job.Episode.Key.Season, job.Episode.Key.Episode)),
		domain.F("mode", string(ModeDirect)),
		domain.F("output", job.OutPath),
		domain.F("size", info.Size()),
	)

	return nil
}

// Execute implements domain.JobExecutor. It delegates to Download with a no-op
// ProgressSink.
func (d *Downloader) Execute(ctx context.Context, job domain.Job) error {
	return d.Download(ctx, job, nil)
}

// estimateDuration returns the expected duration of the media for progress
// computation. It uses the resolved media's duration field obtained from ffprobe.
// Returns 0 if duration cannot be determined.
func estimateDuration(job domain.Job) time.Duration {
	return job.Media.Duration
}

// noopSink is a ProgressSink that discards all updates.
type noopSink struct{}

func (noopSink) TrackProgress(_ domain.EpisodeKey, _ domain.TrackRef, _ int) {}
