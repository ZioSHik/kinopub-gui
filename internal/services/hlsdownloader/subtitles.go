package hlsdownloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

const maxSubtitleBytes = 16 << 20

var _ domain.SubtitleDownloader = (*Downloader)(nil)

// DownloadSubtitles keeps every external subtitle supplied by the API. A
// transient failure is an error so the episode is retried (its segments resume
// from disk); a permanent one (404, empty or oversized file) drops only that
// track with a warning instead of discarding an already downloaded video.
func (d *Downloader) DownloadSubtitles(ctx context.Context, sources []domain.SubtitleSource, dir string) ([]domain.HLSSubtitleTrack, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	if dir == "" {
		return nil, fmt.Errorf("subtitle temporary directory is required")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	var tracks []domain.HLSSubtitleTrack
	seen := make(map[string]bool)
	for _, source := range sources {
		if seen[source.URL] {
			continue
		}
		seen[source.URL] = true
		u, err := url.Parse(source.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			d.logger.Warn("invalid subtitle URL, skipping track", domain.F("language", source.Language))
			continue
		}
		ext := strings.ToLower(filepath.Ext(u.Path))
		switch ext {
		case ".srt", ".vtt", ".ass", ".ssa":
		default:
			ext = ".srt"
		}
		path := filepath.Join(dir, fmt.Sprintf("subtitle_%d%s", len(tracks), ext))
		if err := d.downloadSubtitle(ctx, source.URL, path); err != nil {
			if ctx.Err() != nil || isTransientSubtitleError(err) {
				return nil, fmt.Errorf("subtitle %q: %w", source.Language, err)
			}
			d.logger.Warn("subtitle download failed, skipping track",
				domain.F("language", source.Language),
				domain.F("error", err.Error()),
			)
			continue
		}
		tracks = append(tracks, domain.HLSSubtitleTrack{Path: path, Name: source.Name, Language: source.Language})
	}
	return tracks, nil
}

func (d *Downloader) downloadSubtitle(ctx context.Context, rawURL, path string) error {
	for attempt := 0; ; attempt++ {
		err := d.downloadSubtitleOnce(ctx, rawURL, path)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || !isTransientSubtitleError(err) || attempt >= 2 {
			return err
		}
		delay := time.Second << attempt
		var status *statusError
		if errors.As(err, &status) && status.RetryAfter > delay {
			delay = min(status.RetryAfter, maxThrottleCoolDown)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// isTransientSubtitleError reports CDN/network failures worth retrying.
func isTransientSubtitleError(err error) bool {
	var status *statusError
	var network net.Error
	return (errors.As(err, &status) && (status.throttled() || status.Code >= 500)) ||
		errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func (d *Downloader) downloadSubtitleOnce(ctx context.Context, rawURL, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	applyHLSAuth(req, d.auth)
	resp, err := d.subtitleClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{Code: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".subtitle-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxSubtitleBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n == 0 || n > maxSubtitleBytes {
		return fmt.Errorf("invalid subtitle size: %d bytes", n)
	}
	return os.Rename(tmp, path)
}
