package hlsdownloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// downloadSubtitleRenditions fetches the selected variant's WebVTT renditions.
// As with API subtitles, only transient failures fail the episode; a permanently
// broken rendition is dropped with a warning.
func (d *Downloader) downloadSubtitleRenditions(ctx context.Context, master *MasterPlaylist, selected Variant, dir string) ([]domain.HLSSubtitleTrack, error) {
	if selected.SubtitleGroup == "" || selected.SubtitleGroup == "NONE" {
		return nil, nil
	}
	if dir == "" {
		return nil, fmt.Errorf("subtitle temporary directory is required")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	var renditions []SubtitleRendition
	seen := make(map[string]bool)
	for _, sub := range master.Subtitles {
		if sub.GroupID != selected.SubtitleGroup || sub.URI == "" || seen[sub.URI] {
			continue
		}
		seen[sub.URI] = true
		renditions = append(renditions, sub)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tracks := make([]domain.HLSSubtitleTrack, len(renditions))
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
launch:
	for i, sub := range renditions {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		wg.Add(1)
		go func(i int, sub SubtitleRendition) {
			defer wg.Done()
			defer func() { <-slots }()
			track, err := d.downloadSubtitleRendition(ctx, sub, dir, i)
			if err != nil {
				if ctx.Err() != nil || isTransientSubtitleError(err) {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
				d.logger.Warn("subtitle rendition failed, skipping track",
					domain.F("subtitle", sub.Name),
					domain.F("error", err.Error()),
				)
				return
			}
			tracks[i] = track
		}(i, sub)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	kept := tracks[:0]
	for _, track := range tracks {
		if track.Path != "" {
			kept = append(kept, track)
		}
	}
	return kept, nil
}

func (d *Downloader) downloadSubtitleRendition(ctx context.Context, sub SubtitleRendition, dir string, index int) (domain.HLSSubtitleTrack, error) {
	fail := func(err error) (domain.HLSSubtitleTrack, error) {
		return domain.HLSSubtitleTrack{}, fmt.Errorf("subtitle %q: %w", sub.Name, err)
	}
	playlist, err := FetchMediaPlaylist(ctx, d.subtitleClient, sub.URI, d.auth)
	if err != nil {
		return fail(err)
	}
	if len(playlist.Segments) == 0 {
		return fail(fmt.Errorf("no segments"))
	}
	var merger webVTTMerger
	var total int
	for i, seg := range playlist.Segments {
		part := filepath.Join(dir, fmt.Sprintf(".subtitle-%d-%d.vtt", index, i))
		if err := d.downloadSubtitle(ctx, seg.URL, part); err != nil {
			return fail(err)
		}
		data, err := os.ReadFile(part)
		os.Remove(part)
		if err != nil {
			return fail(err)
		}
		total += len(data)
		if total > maxSubtitleBytes {
			return fail(fmt.Errorf("size limit exceeded"))
		}
		if err := merger.add(string(data)); err != nil {
			return fail(err)
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("subtitle_hls_%d.vtt", index))
	if err := os.WriteFile(path, []byte("WEBVTT\n\n"+merger.body.String()), 0600); err != nil {
		return fail(err)
	}
	return domain.HLSSubtitleTrack{Path: path, Name: sub.Name, Language: sub.Language, Forced: sub.Forced}, nil
}

// WebVTT cue times are already on the presentation timeline, not relative to
// the segment's EXTINF. Preserve them and deduplicate cues spanning a boundary.
// Timestamp maps are normalized to the first segment's MPEGTS/LOCAL mapping.
type webVTTMerger struct {
	body    strings.Builder
	seen    map[string]bool
	mapBase int64
	hasMap  bool
	cues    int
}

func (m *webVTTMerger) add(text string) error {
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff")
	if !strings.HasPrefix(text, "WEBVTT") {
		return fmt.Errorf("expected WebVTT segment")
	}
	var shift int64
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "X-TIMESTAMP-MAP=") {
			continue
		}
		var local, ticks int64
		var haveLocal, haveTicks bool
		for _, field := range strings.Split(strings.TrimPrefix(line, "X-TIMESTAMP-MAP="), ",") {
			key, value, ok := strings.Cut(strings.TrimSpace(field), ":")
			if !ok {
				continue
			}
			var err error
			switch key {
			case "LOCAL":
				local, err = parseVTTTime(value)
				haveLocal = true
			case "MPEGTS":
				ticks, err = strconv.ParseInt(value, 10, 64)
				haveTicks = true
			}
			if err != nil {
				return err
			}
		}
		if !haveLocal || !haveTicks {
			return fmt.Errorf("incomplete WebVTT timestamp map")
		}
		offset := ticks/90 - local
		if !m.hasMap {
			m.mapBase = offset
			m.hasMap = true
		}
		// MPEGTS wraps at 33 bits. Select the nearest mapping to the origin.
		wrap := int64(1<<33) / 90
		for offset-m.mapBase < -wrap/2 {
			offset += wrap
		}
		for offset-m.mapBase > wrap/2 {
			offset -= wrap
		}
		shift = offset - m.mapBase
	}
	if m.seen == nil {
		m.seen = make(map[string]bool)
	}
	for _, block := range strings.Split(text, "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) == 0 || strings.HasPrefix(lines[0], "NOTE") || strings.HasPrefix(lines[0], "STYLE") || strings.HasPrefix(lines[0], "REGION") {
			continue
		}
		for i, line := range lines {
			if !strings.Contains(line, "-->") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[1] != "-->" {
				return fmt.Errorf("invalid WebVTT timing line")
			}
			start, err := parseVTTTime(fields[0])
			if err != nil {
				return err
			}
			end, err := parseVTTTime(fields[2])
			if err != nil {
				return err
			}
			start += shift
			end += shift
			if end <= 0 {
				break
			}
			if start < 0 {
				start = 0
			}
			if end < start {
				return fmt.Errorf("invalid WebVTT cue range")
			}
			timing := formatVTTTime(start) + " --> " + formatVTTTime(end)
			if len(fields) > 3 {
				timing += " " + strings.Join(fields[3:], " ")
			}
			cue := timing + "\n" + strings.Join(lines[i+1:], "\n")
			if !m.seen[cue] {
				m.seen[cue] = true
				m.cues++
				fmt.Fprintf(&m.body, "%d\n%s\n\n", m.cues, cue)
			}
			break
		}
	}
	return nil
}

func parseVTTTime(text string) (int64, error) {
	parts := strings.Split(text, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid WebVTT timestamp %q", text)
	}
	var hours int64
	if len(parts) == 3 {
		var err error
		hours, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || hours < 0 {
			return 0, fmt.Errorf("invalid WebVTT hours")
		}
		parts = parts[1:]
	}
	minutes, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || minutes < 0 || minutes >= 60 {
		return 0, fmt.Errorf("invalid WebVTT minutes")
	}
	seconds, fraction, ok := strings.Cut(parts[1], ".")
	if !ok || len(fraction) != 3 {
		return 0, fmt.Errorf("invalid WebVTT milliseconds")
	}
	sec, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || sec < 0 || sec >= 60 {
		return 0, fmt.Errorf("invalid WebVTT seconds")
	}
	ms, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || ms < 0 {
		return 0, fmt.Errorf("invalid WebVTT milliseconds")
	}
	return ((hours*60+minutes)*60+sec)*1000 + ms, nil
}

func formatVTTTime(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
