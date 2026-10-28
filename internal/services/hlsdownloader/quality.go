// Package hlsdownloader implements HLS segment-based downloading with
// quality selection and resume capability.
package hlsdownloader

import (
	"fmt"
	"strings"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// Variant represents a single quality variant from an HLS master playlist.
type Variant struct {
	Bandwidth     int    // bits per second
	Resolution    string // e.g. "1920x1080"
	Codecs        string // e.g. "avc1.640028,mp4a.40.2"
	URL           string // media playlist URL (relative or absolute)
	Width         int    // parsed from Resolution
	Height        int    // parsed from Resolution
	AudioGroup    string // GROUP-ID of associated audio renditions (empty = muxed)
	SubtitleGroup string // GROUP-ID of external WebVTT renditions
}

// IsH265 reports whether this variant uses HEVC/H.265 codec.
func (v Variant) IsH265() bool {
	return strings.Contains(v.Codecs, "hvc1") ||
		strings.Contains(v.Codecs, "hev1") ||
		strings.Contains(v.Codecs, "hevc")
}

// IsH264 reports whether this variant uses AVC/H.264 codec.
func (v Variant) IsH264() bool {
	return strings.Contains(v.Codecs, "avc1") || v.Codecs == ""
}

// BitrateKbps returns the bandwidth in kbps.
func (v Variant) BitrateKbps() int {
	return v.Bandwidth / 1000
}

// Label returns a human-readable label for this variant.
func (v Variant) Label() string {
	codec := "h264"
	if v.IsH265() {
		codec = "h265"
	}
	return fmt.Sprintf("%dp/%s (%d kbps)", v.qualityHeight(), codec, v.BitrateKbps())
}

// widthClasses maps a standard rendition width to its nominal height.
var widthClasses = map[int]int{3840: 2160, 1920: 1080, 1280: 720, 854: 480, 720: 480, 640: 360}

// qualityHeight retains the rendition class when black bars have been cropped:
// a 1280x534 stream is still the source's 720p rendition, not a 534p tier.
func (v Variant) qualityHeight() int {
	if h := widthClasses[v.Width]; h > v.Height {
		return h
	}
	return v.Height
}

// SelectVariant chooses the best variant based on quality preference.
//
// Quality preference modes:
//   - "" or "optimal": 1080p h264 with bitrate ≤ 3000 kbps, or 720p if unavailable
//   - "max": highest bandwidth variant
//   - "1080p": 1080p with lowest bitrate (h264 preferred)
//   - "720p": 720p with highest bitrate
//   - "480p": 480p
//   - "1080p-h265": specific resolution + codec
func SelectVariant(variants []Variant, pref domain.Quality) (Variant, error) {
	if len(variants) == 0 {
		return Variant{}, fmt.Errorf("no variants available")
	}

	prefStr := strings.ToLower(strings.TrimSpace(string(pref)))

	switch {
	case prefStr == "" || prefStr == "optimal":
		return selectOptimal(variants), nil
	case prefStr == "max":
		return selectMax(variants), nil
	default:
		return selectExplicit(variants, prefStr)
	}
}

// selectOptimal picks the "sweet spot" variant:
// 1. 1080p h264 with bitrate ≤ 3000 kbps (ideal ~2500 kbps)
// 2. If no such variant, 720p h264 with highest bitrate
// 3. If nothing matches, closest to 2500 kbps overall
func selectOptimal(variants []Variant) Variant {
	// Try: 1080p h264 ≤ 3000 kbps
	var candidates []Variant
	for _, v := range variants {
		if v.qualityHeight() == 1080 && v.IsH264() && v.BitrateKbps() <= 3000 {
			candidates = append(candidates, v)
		}
	}
	if len(candidates) > 0 {
		// Pick the one closest to 2500 kbps.
		return closestToBitrate(candidates, 2500)
	}

	// Try: 720p h264
	for _, v := range variants {
		if v.qualityHeight() == 720 && v.IsH264() {
			candidates = append(candidates, v)
		}
	}
	if len(candidates) > 0 {
		// Pick highest bitrate 720p.
		best := candidates[0]
		for _, v := range candidates[1:] {
			if v.Bandwidth > best.Bandwidth {
				best = v
			}
		}
		return best
	}

	// Fallback: closest to 2500 kbps from all variants.
	return closestToBitrate(variants, 2500)
}

// selectMax picks the highest bandwidth variant.
func selectMax(variants []Variant) Variant {
	best := variants[0]
	for _, v := range variants[1:] {
		if v.Bandwidth > best.Bandwidth {
			best = v
		}
	}
	return best
}

// selectExplicit picks a variant matching the explicit preference string.
// Supports: "1080p", "720p", "480p", "1080p-h265", "720p-h264", etc.
func selectExplicit(variants []Variant, pref string) (Variant, error) {
	// Parse preference.
	wantHeight := 0
	wantCodec := "" // "", "h264", "h265"

	if strings.Contains(pref, "-") {
		parts := strings.SplitN(pref, "-", 2)
		pref = parts[0]
		wantCodec = parts[1]
	}

	switch pref {
	case "1080p", "1080":
		wantHeight = 1080
	case "720p", "720":
		wantHeight = 720
	case "480p", "480":
		wantHeight = 480
	case "360p", "360":
		wantHeight = 360
	case "2160p", "4k":
		wantHeight = 2160
	default:
		// Try parsing as number.
		fmt.Sscanf(pref, "%dp", &wantHeight)
		if wantHeight == 0 {
			fmt.Sscanf(pref, "%d", &wantHeight)
		}
	}

	// Filter by height.
	var candidates []Variant
	for _, v := range variants {
		if wantHeight > 0 && v.qualityHeight() != wantHeight {
			continue
		}
		if wantCodec == "h264" && !v.IsH264() {
			continue
		}
		if wantCodec == "h265" && !v.IsH265() {
			continue
		}
		candidates = append(candidates, v)
	}

	if len(candidates) == 0 {
		// A missing resolution may fall back, but an explicit codec must survive.
		fallback := variants
		if wantCodec != "" {
			fallback = nil
			for _, variant := range variants {
				if (wantCodec == "h264" && variant.IsH264()) || (wantCodec == "h265" && variant.IsH265()) {
					fallback = append(fallback, variant)
				}
			}
			if len(fallback) == 0 {
				return Variant{}, fmt.Errorf("requested video codec %s is unavailable", wantCodec)
			}
		}
		return closestToHeight(fallback, wantHeight), nil
	}
	if wantCodec == "" {
		var h264 []Variant
		for _, v := range candidates {
			if v.IsH264() {
				h264 = append(h264, v)
			}
		}
		if len(h264) > 0 {
			candidates = h264
		}
	}

	// Among matches, pick the one with lowest bitrate (most efficient).
	best := candidates[0]
	for _, v := range candidates[1:] {
		if v.Bandwidth < best.Bandwidth {
			best = v
		}
	}
	return best, nil
}

// closestToBitrate returns the variant closest to the target bitrate (in kbps).
func closestToBitrate(variants []Variant, targetKbps int) Variant {
	best := variants[0]
	bestDiff := abs(best.BitrateKbps() - targetKbps)
	for _, v := range variants[1:] {
		diff := abs(v.BitrateKbps() - targetKbps)
		if diff < bestDiff {
			best = v
			bestDiff = diff
		}
	}
	return best
}

// closestToHeight returns the variant closest to the target height.
func closestToHeight(variants []Variant, targetHeight int) Variant {
	best := variants[0]
	bestDiff := abs(best.qualityHeight() - targetHeight)
	for _, v := range variants[1:] {
		diff := abs(v.qualityHeight() - targetHeight)
		if diff < bestDiff || (diff == bestDiff && v.Bandwidth > best.Bandwidth) {
			best = v
			bestDiff = diff
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
