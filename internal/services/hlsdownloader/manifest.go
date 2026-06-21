package hlsdownloader

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Segment represents a single HLS segment from a media playlist.
type Segment struct {
	URL      string
	Duration float64 // seconds
	Index    int
}

// AudioRendition represents an audio track from the master playlist.
type AudioRendition struct {
	GroupID  string
	Name     string
	Language string
	URI      string // media playlist URL for this audio track
}

// MasterPlaylist holds parsed data from an HLS master playlist.
type MasterPlaylist struct {
	Variants []Variant
	Audio    []AudioRendition
}

// MediaPlaylist holds parsed data from an HLS media playlist.
type MediaPlaylist struct {
	Segments      []Segment
	TotalDuration float64 // sum of all segment durations
}

// FetchMasterPlaylist downloads and parses an HLS master playlist.
func FetchMasterPlaylist(ctx context.Context, client *http.Client, manifestURL string, auth domain.RequestAuth, logger domain.Logger) (*MasterPlaylist, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	applyHLSAuth(req, auth)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch master playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("master playlist returned HTTP %d", resp.StatusCode)
	}

	return parseMasterPlaylist(resp.Body, manifestURL)
}

// FetchMediaPlaylist downloads and parses an HLS media playlist (segment list).
// Retries up to 3 times with increasing timeout since CDN can be slow.
func FetchMediaPlaylist(ctx context.Context, client *http.Client, playlistURL string, auth domain.RequestAuth) (*MediaPlaylist, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Increasing timeout: 30s, 45s, 60s.
		timeout := time.Duration(30+attempt*15) * time.Second
		reqCtx, cancel := context.WithTimeout(ctx, timeout)

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, playlistURL, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		applyHLSAuth(req, auth)

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			// Brief pause before retry.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(3 * time.Second):
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			lastErr = fmt.Errorf("media playlist returned HTTP %d", resp.StatusCode)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(3 * time.Second):
			}
			continue
		}

		result, err := parseMediaPlaylist(resp.Body, playlistURL)
		resp.Body.Close()
		cancel()
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	return nil, fmt.Errorf("fetch media playlist: %w", lastErr)
}

// parseMasterPlaylist parses an m3u8 master playlist.
func parseMasterPlaylist(r io.Reader, baseURL string) (*MasterPlaylist, error) {
	scanner := bufio.NewScanner(r)
	result := &MasterPlaylist{}

	var pendingVariant *Variant

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			attrs := line[len("#EXT-X-STREAM-INF:"):]
			v := parseVariantAttrs(attrs)
			pendingVariant = &v
		} else if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			attrs := parseHLSAttributes(line[len("#EXT-X-MEDIA:"):])
			if strings.ToUpper(attrs["TYPE"]) == "AUDIO" {
				rendition := AudioRendition{
					GroupID:  attrs["GROUP-ID"],
					Name:     attrs["NAME"],
					Language: attrs["LANGUAGE"],
					URI:      resolveURL(baseURL, attrs["URI"]),
				}
				result.Audio = append(result.Audio, rendition)
			}
		} else if pendingVariant != nil && !strings.HasPrefix(line, "#") && line != "" {
			pendingVariant.URL = resolveURL(baseURL, line)
			result.Variants = append(result.Variants, *pendingVariant)
			pendingVariant = nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read master playlist: %w", err)
	}

	return result, nil
}

// parseMediaPlaylist parses an m3u8 media playlist (segment list).
func parseMediaPlaylist(r io.Reader, baseURL string) (*MediaPlaylist, error) {
	scanner := bufio.NewScanner(r)
	result := &MediaPlaylist{}

	var pendingDuration float64
	index := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#EXTINF:") {
			// Parse duration: #EXTINF:10.0, or #EXTINF:10.0
			durStr := line[len("#EXTINF:"):]
			if idx := strings.IndexByte(durStr, ','); idx >= 0 {
				durStr = durStr[:idx]
			}
			dur, _ := strconv.ParseFloat(strings.TrimSpace(durStr), 64)
			pendingDuration = dur
		} else if !strings.HasPrefix(line, "#") && line != "" && pendingDuration > 0 {
			seg := Segment{
				URL:      resolveURL(baseURL, line),
				Duration: pendingDuration,
				Index:    index,
			}
			result.Segments = append(result.Segments, seg)
			result.TotalDuration += pendingDuration
			pendingDuration = 0
			index++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read media playlist: %w", err)
	}

	return result, nil
}

// parseVariantAttrs parses #EXT-X-STREAM-INF attributes into a Variant.
func parseVariantAttrs(attrs string) Variant {
	parsed := parseHLSAttributes(attrs)
	var v Variant

	if bw, err := strconv.Atoi(parsed["BANDWIDTH"]); err == nil {
		v.Bandwidth = bw
	}
	if res := parsed["RESOLUTION"]; res != "" {
		v.Resolution = res
		// Parse width x height.
		if parts := strings.SplitN(res, "x", 2); len(parts) == 2 {
			v.Width, _ = strconv.Atoi(parts[0])
			v.Height, _ = strconv.Atoi(parts[1])
		}
	}
	v.Codecs = parsed["CODECS"]
	v.AudioGroup = parsed["AUDIO"]

	return v
}

// parseHLSAttributes parses KEY=VALUE or KEY="VALUE" pairs from an HLS tag.
func parseHLSAttributes(s string) map[string]string {
	result := make(map[string]string)
	s = strings.TrimSpace(s)

	for len(s) > 0 {
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			break
		}
		key := strings.TrimSpace(s[:eqIdx])
		s = s[eqIdx+1:]

		var value string
		if len(s) > 0 && s[0] == '"' {
			s = s[1:]
			endQuote := strings.IndexByte(s, '"')
			if endQuote < 0 {
				value = s
				s = ""
			} else {
				value = s[:endQuote]
				s = s[endQuote+1:]
			}
			if len(s) > 0 && s[0] == ',' {
				s = s[1:]
			}
		} else {
			commaIdx := strings.IndexByte(s, ',')
			if commaIdx < 0 {
				value = s
				s = ""
			} else {
				value = s[:commaIdx]
				s = s[commaIdx+1:]
			}
		}

		result[key] = strings.TrimSpace(value)
	}

	return result
}

// resolveURL resolves a potentially relative URL against a base URL.
func resolveURL(baseURL, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return ref
	}

	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}

	return base.ResolveReference(refURL).String()
}

// applyHLSAuth sets auth headers for HLS requests.
// Unlike progressive downloads, HLS requests to CDN DO need cookies because
// the CDN (cdntogo.net) may require cf_clearance for access.
func applyHLSAuth(req *http.Request, auth domain.RequestAuth) {
	if auth.UserAgent != "" {
		req.Header.Set("User-Agent", auth.UserAgent)
	}
	if auth.Cookie != "" {
		req.Header.Set("Cookie", auth.Cookie)
	}
	for k, v := range auth.Headers {
		req.Header.Set(k, v)
	}
}
