package mediaresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// ffprobeOutput represents the JSON output of ffprobe -show_streams -show_format -print_format json.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

// ffprobeFormat represents the format-level info from ffprobe output.
type ffprobeFormat struct {
	Duration string `json:"duration"` // seconds as a decimal string, e.g. "1423.456"
}

// ffprobeStream represents a single stream from ffprobe output.
type ffprobeStream struct {
	Index     int    `json:"index"`
	CodecType string `json:"codec_type"` // "video", "audio", "subtitle"
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	BitRate   string `json:"bit_rate"`
	Duration  string `json:"duration"` // stream-level duration (seconds)
	Tags      struct {
		Language string `json:"language"`
		Title    string `json:"title"`
		Name     string `json:"name"` // kino.pub uses "name" for studio/track label
	} `json:"tags"`
}

// resolveProgressive runs ffprobe on a progressive (direct) stream to
// enumerate video and audio tracks (Req 3.2).
func (r *Resolver) resolveProgressive(ctx context.Context, source domain.MediaSource) (domain.ResolvedMedia, error) {
	r.logger.Debug("probing progressive stream", domain.F("url", source.URL))

	// CDN URLs (digital-cdn.net, cdn2site.com) are redirectors that check TLS
	// fingerprint and return 403 to non-browser clients (curl, ffprobe). But
	// they redirect browsers to the real CDN (cdntogo.net). We resolve the
	// redirect via Go's HTTP client (which passes the TLS check) and give
	// ffprobe the final URL directly.
	finalURL := source.URL
	if r.client != nil {
		resolved, err := r.resolveRedirect(ctx, source.URL)
		if err != nil {
			r.logger.Debug("redirect resolution failed, using original URL",
				domain.F("error", err.Error()))
		} else if resolved != source.URL {
			r.logger.Debug("resolved CDN redirect",
				domain.F("from", source.URL),
				domain.F("to", resolved))
			finalURL = resolved
		}
	}

	var args []string

	// Inject auth headers into ffprobe so it can access protected CDN URLs.
	// IMPORTANT: Do NOT send Cookie to CDN — it causes timeouts. CDN only needs
	// the Referer header. Cookies are for kino.pub domain only.
	if r.auth.UserAgent != "" {
		args = append(args, "-user_agent", r.auth.UserAgent)
	}
	if len(r.auth.Headers) > 0 {
		var lines []string
		for k, v := range r.auth.Headers {
			lines = append(lines, k+": "+v)
		}
		if len(lines) > 0 {
			args = append(args, "-headers", strings.Join(lines, "\r\n")+"\r\n")
		}
	}

	args = append(args,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		finalURL,
	)

	r.logger.Debug("ffprobe command",
		domain.F("args", fmt.Sprintf("%v", args)),
	)

	output, err := r.runOutput(ctx, "ffprobe", args, nil)
	if err != nil {
		return domain.ResolvedMedia{}, fmt.Errorf("ffprobe: %w", err)
	}

	// Store the final URL in the source so downstream (ffmpeg) uses it too.
	source.URL = finalURL
	return parseFFprobeOutput(output, source)
}

// resolveRedirect follows HTTP redirects via the Go HTTP client and returns
// the final URL. This bypasses CDN redirectors (digital-cdn.net, cdn2site.com)
// that reject non-browser TLS fingerprints with 403, but redirect browsers to
// the real CDN (cdntogo.net). Go's HTTP client passes the TLS check.
func (r *Resolver) resolveRedirect(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return rawURL, err
	}

	// Use a client that does NOT follow redirects — we want to capture the Location.
	noRedirectClient := &http.Client{
		Transport: r.client.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return rawURL, err
	}
	resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return loc, nil
		}
	}

	// If no redirect (200 or error), return original URL.
	// If 403 — the redirector rejected us too. Try with the full client (follows redirects).
	if resp.StatusCode == 403 {
		// Try a full GET with redirect-following client to see if it resolves.
		fullReq, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
		if err != nil {
			return rawURL, err
		}
		fullResp, err := r.client.Do(fullReq)
		if err != nil {
			return rawURL, err
		}
		fullResp.Body.Close()
		// The final URL after redirects is in fullResp.Request.URL.
		if fullResp.Request != nil && fullResp.Request.URL != nil {
			finalURL := fullResp.Request.URL.String()
			if finalURL != rawURL {
				return finalURL, nil
			}
		}
	}

	return rawURL, nil
}

// parseFFprobeOutput parses ffprobe JSON output into a ResolvedMedia.
func parseFFprobeOutput(data []byte, source domain.MediaSource) (domain.ResolvedMedia, error) {
	var probe ffprobeOutput
	if err := json.Unmarshal(data, &probe); err != nil {
		return domain.ResolvedMedia{}, fmt.Errorf("parse ffprobe output: %w", err)
	}

	var video domain.VideoTrack
	var audioTracks []domain.AudioTrack
	var hasVideo bool

	audioIndex := 0

	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if !hasVideo {
				bitRate := 0
				if stream.BitRate != "" {
					if br, err := strconv.Atoi(stream.BitRate); err == nil {
						bitRate = br / 1000 // convert bps to kbps
					}
				}
				video = domain.VideoTrack{
					Index:      stream.Index,
					Resolution: fmt.Sprintf("%dx%d", stream.Width, stream.Height),
					BitRate:    bitRate,
				}
				hasVideo = true
			}
		case "audio":
			// Studio name: prefer "name" tag (kino.pub convention), fall back to "title".
			studio := stream.Tags.Name
			if studio == "" {
				studio = stream.Tags.Title
			}
			track := domain.AudioTrack{
				Index:    audioIndex,
				Language: stream.Tags.Language,
				Studio:   studio,
			}
			audioTracks = append(audioTracks, track)
			audioIndex++
		}
	}

	if !hasVideo {
		return domain.ResolvedMedia{}, domain.ErrNoVideoTrack
	}

	// Parse duration from format-level info (most reliable), falling back to
	// the video stream's duration.
	duration := parseDurationSeconds(probe.Format.Duration)
	if duration == 0 {
		for _, stream := range probe.Streams {
			if stream.CodecType == "video" && stream.Duration != "" {
				duration = parseDurationSeconds(stream.Duration)
				break
			}
		}
	}

	return domain.ResolvedMedia{
		Source:    source,
		Video:    video,
		Audio:    audioTracks,
		Duration: duration,
		// Progressive sources don't have separate subtitle streams in the
		// same way HLS does — subtitle enumeration is HLS-only per design.
		Subtitles: nil,
	}, nil
}

// parseDurationSeconds parses a decimal seconds string (e.g. "1423.456") into
// a time.Duration. Returns 0 if parsing fails.
func parseDurationSeconds(s string) time.Duration {
	if s == "" {
		return 0
	}
	// Parse as float64 seconds.
	var secs float64
	_, err := fmt.Sscanf(s, "%f", &secs)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}
