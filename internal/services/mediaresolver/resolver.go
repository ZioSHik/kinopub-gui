// Package mediaresolver implements domain.MediaResolver — it dispatches HLS
// vs progressive media resolution and provides quality-based source selection.
package mediaresolver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Compile-time interface assertion.
var _ domain.MediaResolver = (*Resolver)(nil)

// resolveTimeout is the maximum time allowed for a single Resolve call (Req 3.8).
const resolveTimeout = 30 * time.Second

// RunOutputFunc is a function that runs a command and returns its stdout.
// This is used for ffprobe invocations where we need captured output.
type RunOutputFunc func(ctx context.Context, name string, args, env []string) ([]byte, error)

// Resolver implements domain.MediaResolver.
type Resolver struct {
	client    *http.Client
	runOutput RunOutputFunc
	logger    domain.Logger
	auth      domain.RequestAuth
}

// New creates a new Resolver.
//   - client: HTTP client for fetching HLS playlists (should already carry auth)
//   - runOutput: function to run ffprobe and capture stdout
//   - logger: structured logger
//   - auth: request auth to pass to ffprobe via -headers/-user_agent
func New(client *http.Client, runOutput RunOutputFunc, logger domain.Logger, auth ...domain.RequestAuth) *Resolver {
	r := &Resolver{
		client:    client,
		runOutput: runOutput,
		logger:    logger.Component("mediaresolver"),
	}
	if len(auth) > 0 {
		r.auth = auth[0]
	}
	return r
}

// Resolve enumerates tracks for an episode within a 30s timeout (Req 3.8).
// It selects the MediaSource by quality preference, else highest quality
// (Req 3.6, 3.7). Returns ErrNoVideoTrack if no video track resolves (Req 3.5).
func (r *Resolver) Resolve(ctx context.Context, ep domain.Episode, pref domain.QualityPref) (domain.ResolvedMedia, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	r.logger.Info("resolving media",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", ep.Key.Season, ep.Key.Episode)),
	)

	if len(ep.MediaSources) == 0 {
		return domain.ResolvedMedia{}, domain.ErrNoVideoTrack
	}

	source := SelectMediaSource(ep.MediaSources, pref)

	var resolved domain.ResolvedMedia
	var err error

	switch source.Kind {
	case domain.MediaHLS:
		resolved, err = r.resolveHLS(ctx, source)
	case domain.MediaProgressive:
		resolved, err = r.resolveProgressive(ctx, source)
	default:
		return domain.ResolvedMedia{}, fmt.Errorf("unknown media kind: %d", source.Kind)
	}

	if err != nil {
		return domain.ResolvedMedia{}, err
	}

	// Verify we have at least one video track (Req 3.5).
	if resolved.Video.Resolution == "" && resolved.Video.Bandwidth == 0 {
		return domain.ResolvedMedia{}, domain.ErrNoVideoTrack
	}

	r.logger.Info("media resolved",
		domain.F("video", resolved.Video.Resolution),
		domain.F("bitrate_kbps", resolved.Video.BitRate),
		domain.F("audio_tracks", len(resolved.Audio)),
		domain.F("subtitle_tracks", len(resolved.Subtitles)),
	)

	return resolved, nil
}

// SelectMediaSource is a pure function that selects the best media source
// based on quality preference (Req 3.6, 3.7).
//
// If pref matches a source's Quality exactly, that source is returned.
// Otherwise, the source with the highest quality is returned (by parsing
// resolution height or bandwidth).
func SelectMediaSource(sources []domain.MediaSource, pref domain.QualityPref) domain.MediaSource {
	if len(sources) == 0 {
		return domain.MediaSource{}
	}

	// If a preference is set, look for an exact match.
	if pref != "" {
		for _, s := range sources {
			if s.Quality == string(pref) {
				return s
			}
		}
	}

	// No exact match or no preference — pick highest quality.
	best := sources[0]
	bestScore := qualityScore(best.Quality)
	for _, s := range sources[1:] {
		score := qualityScore(s.Quality)
		if score > bestScore {
			best = s
			bestScore = score
		}
	}
	return best
}

// qualityScore parses a quality string like "1080p", "720p", "4k" into a
// numeric score for comparison. Higher is better.
func qualityScore(q string) int {
	q = strings.TrimSpace(strings.ToLower(q))

	// Handle common named qualities.
	switch q {
	case "4k", "2160p":
		return 2160
	case "1440p":
		return 1440
	case "1080p":
		return 1080
	case "720p":
		return 720
	case "480p":
		return 480
	case "360p":
		return 360
	case "240p":
		return 240
	}

	// Try to parse numeric prefix (e.g., "1080p" → 1080).
	q = strings.TrimSuffix(q, "p")
	if n, err := strconv.Atoi(q); err == nil {
		return n
	}

	return 0
}
