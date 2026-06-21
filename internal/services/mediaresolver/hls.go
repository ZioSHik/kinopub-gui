package mediaresolver

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// resolveHLS fetches and parses an m3u8 master playlist to enumerate
// video variants, audio tracks, and subtitle tracks (Req 3.1, 3.3, 3.4).
func (r *Resolver) resolveHLS(ctx context.Context, source domain.MediaSource) (domain.ResolvedMedia, error) {
	r.logger.Debug("fetching HLS master playlist", domain.F("url", source.URL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return domain.ResolvedMedia{}, fmt.Errorf("create HLS request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return domain.ResolvedMedia{}, fmt.Errorf("fetch HLS playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return domain.ResolvedMedia{}, fmt.Errorf("HLS playlist returned status %d", resp.StatusCode)
	}

	return parseM3U8(resp.Body, source)
}

// parseM3U8 parses an m3u8 master playlist from a reader.
// It extracts #EXT-X-STREAM-INF variants (video) and #EXT-X-MEDIA tags
// (audio/subtitles).
func parseM3U8(r io.Reader, source domain.MediaSource) (domain.ResolvedMedia, error) {
	scanner := bufio.NewScanner(r)

	var video domain.VideoTrack
	var audioTracks []domain.AudioTrack
	var subtitleTracks []domain.SubtitleTrack
	var hasVideo bool

	audioIndex := 0
	subtitleIndex := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			// Parse video variant.
			attrs := line[len("#EXT-X-STREAM-INF:"):]
			v := parseStreamInf(attrs)
			// Use the first (or highest bandwidth) variant as the video track.
			if !hasVideo || v.Bandwidth > video.Bandwidth {
				video = v
				hasVideo = true
			}
		} else if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			attrs := line[len("#EXT-X-MEDIA:"):]
			mediaAttrs := parseAttributes(attrs)

			mediaType := strings.ToUpper(mediaAttrs["TYPE"])
			switch mediaType {
			case "AUDIO":
				track := domain.AudioTrack{
					Index:    audioIndex,
					GroupID:  mediaAttrs["GROUP-ID"],
					Language: mediaAttrs["LANGUAGE"],
					Studio:   mediaAttrs["NAME"],
				}
				audioTracks = append(audioTracks, track)
				audioIndex++
			case "SUBTITLES":
				track := domain.SubtitleTrack{
					Index:    subtitleIndex,
					GroupID:  mediaAttrs["GROUP-ID"],
					Language: mediaAttrs["LANGUAGE"],
					Source:   mediaAttrs["NAME"],
				}
				subtitleTracks = append(subtitleTracks, track)
				subtitleIndex++
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.ResolvedMedia{}, fmt.Errorf("read HLS playlist: %w", err)
	}

	if !hasVideo {
		return domain.ResolvedMedia{}, domain.ErrNoVideoTrack
	}

	return domain.ResolvedMedia{
		Source:    source,
		Video:     video,
		Audio:     audioTracks,
		Subtitles: subtitleTracks,
	}, nil
}

// parseStreamInf parses #EXT-X-STREAM-INF attributes into a VideoTrack.
func parseStreamInf(attrs string) domain.VideoTrack {
	parsed := parseAttributes(attrs)

	var track domain.VideoTrack

	if bw, err := strconv.Atoi(parsed["BANDWIDTH"]); err == nil {
		track.Bandwidth = bw
	}

	if res := parsed["RESOLUTION"]; res != "" {
		track.Resolution = res
	}

	return track
}

// parseAttributes parses a comma-separated list of KEY=VALUE or KEY="VALUE"
// attributes from an HLS tag line.
func parseAttributes(s string) map[string]string {
	result := make(map[string]string)
	s = strings.TrimSpace(s)

	for len(s) > 0 {
		// Find key.
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			break
		}
		key := strings.TrimSpace(s[:eqIdx])
		s = s[eqIdx+1:]

		var value string
		if len(s) > 0 && s[0] == '"' {
			// Quoted value.
			s = s[1:]
			endQuote := strings.IndexByte(s, '"')
			if endQuote < 0 {
				value = s
				s = ""
			} else {
				value = s[:endQuote]
				s = s[endQuote+1:]
			}
			// Skip comma after quoted value.
			if len(s) > 0 && s[0] == ',' {
				s = s[1:]
			}
		} else {
			// Unquoted value — ends at next comma.
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
