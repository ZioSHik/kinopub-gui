// Package mediainfo recovers local container metadata without changing media files.
package mediainfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type fingerprint struct {
	path     string
	size     int64
	modified int64
}

var audioCache = struct {
	sync.Mutex
	entries map[fingerprint][]string
}{entries: make(map[fingerprint][]string)}

// AudioNames reads titles embedded in an existing MKV/MP4. Missing metadata stays
// unknown: a studio must never be inferred from the current catalog selection.
// Results are cached by file size and modification time; remuxing invalidates them.
func AudioNames(ctx context.Context, path string) []string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	key := fingerprint{absolute, info.Size(), info.ModTime().UnixNano()}
	audioCache.Lock()
	names, found := audioCache.entries[key]
	audioCache.Unlock()
	if found {
		return append([]string(nil), names...)
	}
	if ctx.Err() != nil || !isContainer(path) {
		return nil
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, probe, "-v", "error", "-protocol_whitelist", "file,pipe",
		"-select_streams", "a", "-show_entries", "stream=index,codec_name,channels:stream_tags=title,language", "-of", "json", absolute).Output()
	if err != nil {
		return nil
	}
	var result struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Channels int    `json:"channels"`
			Tags     struct {
				Title    string `json:"title"`
				Language string `json:"language"`
			} `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(output, &result) != nil {
		return nil
	}
	names = nil
	for i, stream := range result.Streams {
		name := strings.TrimSpace(stream.Tags.Title)
		if name == "" {
			name = fmt.Sprintf("Track %d", i+1)
			if stream.Tags.Language != "" {
				name += " (" + stream.Tags.Language + ")"
			}
		}
		if stream.Codec != "" && stream.Channels > 0 {
			name = audioDetails.ReplaceAllString(name, "")
			name += fmt.Sprintf(" [%s %dch]", strings.ToUpper(stream.Codec), stream.Channels)
		}
		names = append(names, name)
	}
	audioCache.Lock()
	if len(audioCache.entries) >= 1024 {
		audioCache.entries = make(map[fingerprint][]string)
	}
	audioCache.entries[key] = append([]string(nil), names...)
	audioCache.Unlock()
	return names
}

var audioDetails = regexp.MustCompile(`\s*\[[^\]]+ \d+ch\]$`)
var genericTitle = regexp.MustCompile(`(?i)^(?:track|дорожка)\s+\d+(?:\s*\([^)]*\))?\s*\[[^\]]+ \d+ch\]$`)

// MP4 can omit stream titles. Preserve recorded studio names in that case and
// enrich only their codec/layout, using the persisted order when counts agree.
func EnrichAudioNames(ctx context.Context, path string, known []string) []string {
	actual := AudioNames(ctx, path)
	if len(actual) == 0 {
		return append([]string(nil), known...)
	}
	if len(actual) == len(known) {
		for i, name := range actual {
			if genericTitle.MatchString(name) && known[i] != "" {
				details := audioDetails.FindString(name)
				actual[i] = audioDetails.ReplaceAllString(known[i], "") + details
			}
		}
	}
	return actual
}

func isContainer(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var header [12]byte
	n, _ := io.ReadFull(f, header[:])
	return n >= 4 && string(header[:4]) == "\x1a\x45\xdf\xa3" || n >= 8 && string(header[4:8]) == "ftyp"
}
