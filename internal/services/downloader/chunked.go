// Package downloader — chunked.go implements resumable HTTP streaming download
// for progressive MP4 sources. It downloads the raw video file in a single
// streaming GET request (with Range header for resume), writing directly to a
// .part file. On network failure, it retries from the last written byte.
// This approach matches yt-dlp behavior: one long-lived connection, no
// per-chunk overhead, instant resume on interruption.
package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

const (
	// maxResumeAttempts is the maximum number of retry attempts before giving up.
	maxResumeAttempts = 10

	// retryBaseDelay is the initial delay between retries (doubles each attempt).
	retryBaseDelay = 3 * time.Second

	// progressReportInterval controls how often we report progress (by bytes).
	// Report every 512KB downloaded.
	progressReportInterval = 512 * 1024
)

// ChunkedDownloader downloads files using HTTP streaming with resume capability.
// It opens a single GET connection and streams the body to disk. On failure,
// it resumes from the last written byte using Range headers.
type ChunkedDownloader struct {
	client *http.Client
	auth   domain.RequestAuth
	logger domain.Logger
}

// NewChunked creates a ChunkedDownloader with the given HTTP client and auth.
// The client's Timeout is cleared because streaming downloads are long-lived
// and should not have a global deadline.
func NewChunked(client *http.Client, auth domain.RequestAuth, logger domain.Logger) *ChunkedDownloader {
	// Create a copy of the client without the global Timeout.
	noTimeoutClient := *client
	noTimeoutClient.Timeout = 0

	return &ChunkedDownloader{
		client: &noTimeoutClient,
		auth:   auth,
		logger: logger.Component("chunked"),
	}
}

// CanHandle reports whether this source can be downloaded via streaming HTTP.
// Only progressive (direct MP4/MKV) sources with a known URL are supported.
func (c *ChunkedDownloader) CanHandle(media domain.ResolvedMedia) bool {
	return media.Source.Kind == domain.MediaProgressive && media.Source.URL != ""
}

// Download fetches the raw media file using HTTP streaming with resume.
// It writes to outPath+".part" during download and renames to outPath on
// completion. The sink receives progress updates based on bytes downloaded.
//
// On failure, the .part file is preserved for resume on the next attempt.
func (c *ChunkedDownloader) Download(ctx context.Context, url string, outPath string, key domain.EpisodeKey, sink domain.ProgressSink) error {
	partPath := outPath + ".part"

	// 1. Determine total file size via HEAD request.
	totalSize, err := c.probeSize(ctx, url)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	c.logger.Info("streaming download starting",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)),
		domain.F("total_size", formatBytes(totalSize)),
	)

	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}

	// 2. Check existing partial file.
	offset := c.getPartialOffset(partPath, totalSize)

	// Already complete?
	if offset >= totalSize && totalSize > 0 {
		c.logger.Info("file already complete, renaming",
			domain.F("episode", fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)),
		)
		if sink != nil {
			sink.TrackProgress(key, track, 100)
			if byteSink, ok := sink.(domain.ByteProgressSink); ok {
				byteSink.ByteProgress(key, totalSize, totalSize)
			}
		}
		return os.Rename(partPath, outPath)
	}

	// Report initial progress if resuming.
	if offset > 0 && sink != nil && totalSize > 0 {
		c.logger.Info("resuming download",
			domain.F("episode", fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)),
			domain.F("offset", formatBytes(offset)),
			domain.F("remaining", formatBytes(totalSize-offset)),
		)
		pct := int(offset * 100 / totalSize)
		sink.TrackProgress(key, track, pct)
		if byteSink, ok := sink.(domain.ByteProgressSink); ok {
			byteSink.ByteProgress(key, offset, totalSize)
		}
	}

	// 3. Stream with retry loop.
	consecutiveFailures := 0

	for offset < totalSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Open streaming connection from current offset.
		n, err := c.streamFrom(ctx, url, partPath, offset, totalSize, key, track, sink)
		offset += n

		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			// If we haven't downloaded anything at all, fail fast (URL expired).
			if offset == 0 && n == 0 {
				return fmt.Errorf("download failed immediately (URL may be expired): %w", err)
			}

			consecutiveFailures++
			if consecutiveFailures >= maxResumeAttempts {
				return fmt.Errorf("download failed after %d retries at %s: %w",
					maxResumeAttempts, formatBytes(offset), err)
			}

			c.logger.Warn("connection lost, resuming",
				domain.F("episode", fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)),
				domain.F("downloaded", formatBytes(offset)),
				domain.F("attempt", consecutiveFailures),
				domain.F("error", err.Error()),
			)

			// Exponential backoff: 3s, 6s, 12s, ... capped at 30s.
			delay := retryBaseDelay * time.Duration(1<<(consecutiveFailures-1))
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		// Stream completed successfully.
		consecutiveFailures = 0
	}

	// 4. Verify and rename.
	info, err := os.Stat(partPath)
	if err != nil {
		return fmt.Errorf("stat part file: %w", err)
	}
	if totalSize > 0 && info.Size() != totalSize {
		return fmt.Errorf("size mismatch: got %d, expected %d", info.Size(), totalSize)
	}

	if err := os.Rename(partPath, outPath); err != nil {
		return fmt.Errorf("rename part to final: %w", err)
	}

	c.logger.Info("download complete",
		domain.F("episode", fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)),
		domain.F("size", formatBytes(totalSize)),
	)

	return nil
}

// streamFrom opens a single GET connection starting at offset and streams
// the body to the .part file. Returns bytes written in this session and any
// error (nil means stream completed to EOF).
func (c *ChunkedDownloader) streamFrom(
	ctx context.Context,
	url, partPath string,
	offset, totalSize int64,
	key domain.EpisodeKey,
	track domain.TrackRef,
	sink domain.ProgressSink,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	c.applyAuth(req)

	// Request from offset to end.
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Validate response.
	switch resp.StatusCode {
	case http.StatusOK:
		// Full file from start — only valid if offset is 0.
		if offset > 0 {
			// Server ignored Range — can't resume. Start fresh.
			offset = 0
		}
	case http.StatusPartialContent:
		// Expected for Range requests.
	case http.StatusRequestedRangeNotSatisfiable:
		// Already past the end.
		return 0, nil
	default:
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Open file for writing at offset.
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("open part file: %w", err)
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek: %w", err)
	}

	// Stream body to file with progress reporting.
	buf := make([]byte, 64*1024) // 64KB read buffer
	var written int64
	var lastReport int64

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			nw, writeErr := f.Write(buf[:n])
			written += int64(nw)
			if writeErr != nil {
				return written, writeErr
			}

			// Report progress periodically.
			if sink != nil && totalSize > 0 && (written-lastReport) >= progressReportInterval {
				lastReport = written
				currentTotal := offset + written
				pct := int(currentTotal * 100 / totalSize)
				if pct > 100 {
					pct = 100
				}
				sink.TrackProgress(key, track, pct)
				if byteSink, ok := sink.(domain.ByteProgressSink); ok {
					byteSink.ByteProgress(key, currentTotal, totalSize)
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				// Final progress report.
				if sink != nil && totalSize > 0 {
					currentTotal := offset + written
					pct := int(currentTotal * 100 / totalSize)
					if pct > 100 {
						pct = 100
					}
					sink.TrackProgress(key, track, pct)
					if byteSink, ok := sink.(domain.ByteProgressSink); ok {
						byteSink.ByteProgress(key, currentTotal, totalSize)
					}
				}
				return written, nil
			}
			return written, readErr
		}
	}
}

// getPartialOffset returns the size of an existing .part file, or 0 if none.
// If the partial file is larger than totalSize, it's deleted and 0 is returned.
func (c *ChunkedDownloader) getPartialOffset(partPath string, totalSize int64) int64 {
	info, err := os.Stat(partPath)
	if err != nil {
		return 0
	}
	offset := info.Size()
	if totalSize > 0 && offset > totalSize {
		c.logger.Warn("partial file larger than expected, starting fresh",
			domain.F("partial_size", offset),
			domain.F("server_size", totalSize),
		)
		os.Remove(partPath)
		return 0
	}
	return offset
}

// probeSize sends a HEAD request to determine the file size.
// Returns 0 if size cannot be determined (download will still work but
// without progress percentage).
func (c *ChunkedDownloader) probeSize(ctx context.Context, url string) (int64, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	c.applyAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HEAD returned HTTP %d", resp.StatusCode)
	}

	size := resp.ContentLength
	if size <= 0 {
		clStr := resp.Header.Get("Content-Length")
		if clStr != "" {
			size, _ = strconv.ParseInt(clStr, 10, 64)
		}
	}

	return size, nil
}

// applyAuth sets authentication headers on the request.
// NOTE: Cookie is NOT sent to CDN hosts (cdntogo.net, etc.) — it causes
// throttling and timeouts. Only User-Agent and extra headers (Referer) are sent.
// Cookie is only needed for kino.pub domain requests.
func (c *ChunkedDownloader) applyAuth(req *http.Request) {
	if c.auth.UserAgent != "" {
		req.Header.Set("User-Agent", c.auth.UserAgent)
	}
	// Only send Cookie to kino.pub, not to CDN hosts.
	if c.auth.Cookie != "" && strings.Contains(req.URL.Host, "kino.pub") {
		req.Header.Set("Cookie", c.auth.Cookie)
	}
	for k, v := range c.auth.Headers {
		req.Header.Set(k, v)
	}
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// truncateURL shortens a URL for logging (removes query params).
func truncateURL(url string) string {
	if idx := strings.Index(url, "?"); idx > 0 {
		return url[:idx] + "?..."
	}
	if len(url) > 80 {
		return url[:77] + "..."
	}
	return url
}
