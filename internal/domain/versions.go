package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Files flattens the current and earlier files for one logical episode.
func (r CompletedRec) Files() []CompletedRec {
	versions := r.Versions
	r.Versions = nil
	return append([]CompletedRec{r}, versions...)
}

// MatchesVideo compares the requested rendition and container, including legacy
// recordings that only contain actual dimensions. Cropped widths retain their tier.
func (r CompletedRec) MatchesVideo(quality Quality, container Container) bool {
	ext := ".mkv"
	if container == ContainerMP4 {
		ext = ".mp4"
	}
	if !strings.EqualFold(filepath.Ext(r.Path), ext) {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(string(quality)))
	if q == "" {
		q = "optimal"
	}
	recorded := r.RequestedQuality
	if recorded == "" {
		recorded = "optimal"
	}
	if q == recorded && r.RequestedQuality != "" {
		return true
	}
	var w, h int
	fmt.Sscanf(r.Resolution, "%dx%d", &w, &h)
	if w == 0 {
		fmt.Sscanf(r.Quality, "%dx%d", &w, &h)
	}
	for width, tier := range map[int]int{3840: 2160, 1920: 1080, 1280: 720, 854: 480, 720: 480, 640: 360} {
		if w == width && tier > h {
			h = tier
		}
	}
	parts := strings.SplitN(q, "-", 2)
	if parts[0] != fmt.Sprintf("%dp", h) {
		return false
	}
	return len(parts) == 1 || strings.Contains(r.Quality, "/"+parts[1])
}
