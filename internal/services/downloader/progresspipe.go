// Package downloader — progresspipe.go parses ffmpeg -progress key=value output
// and reports per-track download percentage via a ProgressSink (Req 10.3).
package downloader

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// progressParser implements io.Writer and parses ffmpeg -progress output.
// It computes per-track percentage from out_time / total duration and reports
// via the provided ProgressSink.
type progressParser struct {
	sink     domain.ProgressSink
	key      domain.EpisodeKey
	track    domain.TrackRef
	duration time.Duration // total expected duration for percentage computation

	// Internal buffering for line-based parsing.
	reader *io.PipeReader
	writer *io.PipeWriter
	done   chan struct{}

	// Track the last reported percent for truncation detection.
	mu         sync.Mutex
	lastPctVal int
}

// newProgressParser creates a progressParser that writes parsed progress to sink.
// duration is the total expected duration of the media for percentage computation.
// If duration is zero, no percentage updates are emitted.
func newProgressParser(sink domain.ProgressSink, key domain.EpisodeKey, track domain.TrackRef, duration time.Duration) *progressParser {
	pr, pw := io.Pipe()
	p := &progressParser{
		sink:     sink,
		key:      key,
		track:    track,
		duration: duration,
		reader:   pr,
		writer:   pw,
		done:     make(chan struct{}),
	}
	go p.parse()
	return p
}

// Write implements io.Writer. It forwards bytes to the internal pipe for parsing.
func (p *progressParser) Write(data []byte) (int, error) {
	return p.writer.Write(data)
}

// Close signals that no more data will be written and waits for parsing to finish.
func (p *progressParser) Close() error {
	err := p.writer.Close()
	<-p.done
	return err
}

// parse reads lines from the pipe and processes key=value pairs.
func (p *progressParser) parse() {
	defer close(p.done)

	scanner := bufio.NewScanner(p.reader)
	for scanner.Scan() {
		line := scanner.Text()
		p.processLine(line)
	}
}

// processLine handles a single key=value line from ffmpeg -progress output.
func (p *progressParser) processLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}

	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	switch key {
	case "out_time":
		p.handleOutTime(value)
	case "out_time_us":
		p.handleOutTimeUS(value)
	}
}

// handleOutTime parses HH:MM:SS.us format and reports progress.
func (p *progressParser) handleOutTime(value string) {
	d := parseOutTime(value)
	if d > 0 {
		p.reportProgress(d)
	}
}

// handleOutTimeUS parses microseconds and reports progress.
func (p *progressParser) handleOutTimeUS(value string) {
	us, err := strconv.ParseInt(value, 10, 64)
	if err != nil || us <= 0 {
		return
	}
	d := time.Duration(us) * time.Microsecond
	p.reportProgress(d)
}

// reportProgress computes percentage and reports to the sink.
func (p *progressParser) reportProgress(elapsed time.Duration) {
	if p.duration <= 0 || p.sink == nil {
		return
	}

	percent := int(elapsed * 100 / p.duration)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	p.mu.Lock()
	p.lastPctVal = percent
	p.mu.Unlock()

	p.sink.TrackProgress(p.key, p.track, percent)
}

// lastPercent returns the last reported progress percentage.
// Used by the downloader to detect truncated downloads.
func (p *progressParser) lastPercent() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastPctVal
}

// parseOutTime parses ffmpeg's out_time format: HH:MM:SS.microseconds
// Example: "01:23:45.678900" → 1h23m45.6789s
func parseOutTime(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	// Split on ':'
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0
	}

	minutes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}

	// Seconds part may have fractional component.
	secStr := parts[2]
	var seconds int64
	var microseconds int64

	if dotIdx := strings.Index(secStr, "."); dotIdx >= 0 {
		seconds, err = strconv.ParseInt(secStr[:dotIdx], 10, 64)
		if err != nil {
			return 0
		}
		fracStr := secStr[dotIdx+1:]
		// Pad or truncate to 6 digits (microseconds).
		for len(fracStr) < 6 {
			fracStr += "0"
		}
		if len(fracStr) > 6 {
			fracStr = fracStr[:6]
		}
		microseconds, err = strconv.ParseInt(fracStr, 10, 64)
		if err != nil {
			return 0
		}
	} else {
		seconds, err = strconv.ParseInt(secStr, 10, 64)
		if err != nil {
			return 0
		}
	}

	d := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(microseconds)*time.Microsecond

	return d
}
