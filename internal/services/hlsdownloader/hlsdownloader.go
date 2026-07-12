// Package hlsdownloader implements HLS segment-based downloading with
// quality selection, per-segment retry, and resume capability.
package hlsdownloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/httpx"
)

const (
	// maxSegmentRetries is the max retry count for a single segment.
	maxSegmentRetries = 5

	// segmentRetryDelay is the base delay between segment retries.
	segmentRetryDelay = 2 * time.Second

	// defaultConcurrency is the default number of segments fetched in parallel
	// across all tracks of an episode.
	defaultConcurrency = 4
)

// Downloader downloads HLS streams by fetching individual segments.
type Downloader struct {
	client      *http.Client
	auth        domain.RequestAuth
	logger      domain.Logger
	concurrency int
	proxyURL    *url.URL

	mu        sync.RWMutex
	audioPref domain.AudioPreference
}

// Option configures the Downloader.
type Option func(*Downloader)

// WithConcurrency sets the number of segments fetched in parallel across all
// tracks (video + audio) of an episode. Values < 1 fall back to the default.
func WithConcurrency(n int) Option {
	return func(d *Downloader) {
		if n >= 1 {
			d.concurrency = n
		}
	}
}

// WithProxy sets the proxy URL for CDN segment requests.
func WithProxy(proxyURL *url.URL) Option {
	return func(d *Downloader) {
		d.proxyURL = proxyURL
	}
}

// New creates a new HLS Downloader.
// It uses a browser-fingerprint HTTP client (uTLS) to bypass CDN throttling.
func New(client *http.Client, auth domain.RequestAuth, logger domain.Logger, opts ...Option) *Downloader {
	d := &Downloader{
		auth:        auth,
		logger:      logger.Component("hls"),
		concurrency: defaultConcurrency,
	}
	for _, o := range opts {
		o(d)
	}
	// Use browser-fingerprint client for CDN requests, routing through proxy if set.
	// The regular Go HTTP client gets throttled by Cloudflare/CDN due to
	// its distinctive TLS fingerprint.
	d.client = httpx.NewBrowserClient(d.proxyURL)
	return d
}

// DownloadResult contains info about the completed download (internal use).
type DownloadResult struct {
	SelectedVariant Variant
	TotalSegments   int
	TotalBytes      int64
}

// DownloadEpisode downloads an episode via HLS segments.
// It fetches the master playlist, selects quality, downloads all segments,
// concatenates them into a single .ts file at outPath.
//
// The caller is responsible for remuxing the .ts file into the final container.
func (d *Downloader) DownloadEpisode(
	ctx context.Context,
	manifestURL string,
	quality domain.Quality,
	outPath string,
	key domain.EpisodeKey,
	sink domain.ProgressSink,
) (*domain.HLSDownloadResult, error) {
	return d.downloadEpisodeInternal(ctx, manifestURL, quality, outPath, key, sink)
}

// SetAudioPreference sets the audio-track filter applied to subsequent
// DownloadEpisode calls. Safe for concurrent use.
func (d *Downloader) SetAudioPreference(pref domain.AudioPreference) {
	d.mu.Lock()
	d.audioPref = pref
	d.mu.Unlock()
}

// audioPreference returns the current audio preference under a read lock.
func (d *Downloader) audioPreference() domain.AudioPreference {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.audioPref
}

// ListAudioTracks fetches the master playlist and reports the audio renditions
// available for the selected quality variant, without downloading segments.
func (d *Downloader) ListAudioTracks(ctx context.Context, manifestURL string, quality domain.Quality) ([]domain.AudioTrackInfo, error) {
	master, err := FetchMasterPlaylist(ctx, d.client, manifestURL, d.auth, d.logger)
	if err != nil {
		return nil, fmt.Errorf("master playlist: %w", err)
	}
	if len(master.Variants) == 0 {
		return nil, fmt.Errorf("no variants found in master playlist")
	}
	selected, err := SelectVariant(master.Variants, quality)
	if err != nil {
		return nil, fmt.Errorf("quality selection: %w", err)
	}
	renditions := audioRenditionsFor(master, selected)
	infos := make([]domain.AudioTrackInfo, len(renditions))
	for i, a := range renditions {
		infos[i] = domain.AudioTrackInfo{Index: i, Name: a.Name, Language: a.Language}
	}
	return infos, nil
}

// audioRenditionsFor returns the audio renditions belonging to the selected
// variant's audio group (in master-playlist order), excluding entries with no
// media URI. When the variant has no audio group, the result is empty (audio is
// muxed into the video stream).
func audioRenditionsFor(master *MasterPlaylist, selected Variant) []AudioRendition {
	var out []AudioRendition
	if selected.AudioGroup == "" {
		return out
	}
	// kino.pub's mixed-codec (4K) masters list each dub twice inside one group —
	// once for the AVC video, once for the HEVC video — under the identical NAME.
	// Deduplicate by name+language so a single picked dub isn't downloaded twice.
	seen := make(map[string]bool)
	for _, a := range master.Audio {
		if a.GroupID != selected.AudioGroup || a.URI == "" {
			continue
		}
		key := a.Name + "\x00" + a.Language
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// downloadEpisodeInternal downloads video segments and (for demuxed HLS) audio
// segments separately. It returns the local paths so the caller can mux them
// together with ffmpeg. The caller is responsible for removing result.TempDir.
func (d *Downloader) downloadEpisodeInternal(
	ctx context.Context,
	manifestURL string,
	quality domain.Quality,
	outPath string,
	key domain.EpisodeKey,
	sink domain.ProgressSink,
) (*domain.HLSDownloadResult, error) {
	epLabel := fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)

	// 1. Fetch and parse master playlist.
	d.logger.Info("fetching HLS master playlist", domain.F("episode", epLabel))

	master, err := FetchMasterPlaylist(ctx, d.client, manifestURL, d.auth, d.logger)
	if err != nil {
		return nil, fmt.Errorf("master playlist: %w", err)
	}
	if len(master.Variants) == 0 {
		return nil, fmt.Errorf("no variants found in master playlist")
	}

	// Log available qualities.
	var qualityLabels []string
	for _, v := range master.Variants {
		qualityLabels = append(qualityLabels, v.Label())
	}
	d.logger.Info("available qualities",
		domain.F("episode", epLabel),
		domain.F("variants", strings.Join(qualityLabels, ", ")),
	)

	// 2. Select quality variant.
	selected, err := SelectVariant(master.Variants, quality)
	if err != nil {
		return nil, fmt.Errorf("quality selection: %w", err)
	}

	// Determine which audio renditions belong to the selected variant.
	allRenditions := audioRenditionsFor(master, selected)

	// Apply the audio-track preference (selection / filtering). The preference
	// is matched against rendition names and languages; an empty preference
	// keeps every track.
	pref := d.audioPreference()
	audioRenditions := allRenditions
	if !pref.IsAll() && len(allRenditions) > 0 {
		infos := make([]domain.AudioTrackInfo, len(allRenditions))
		for i, a := range allRenditions {
			infos[i] = domain.AudioTrackInfo{Index: i, Name: a.Name, Language: a.Language}
		}
		keep := domain.SelectAudio(infos, pref)
		filtered := make([]AudioRendition, 0, len(keep))
		var keptLabels []string
		for _, idx := range keep {
			filtered = append(filtered, allRenditions[idx])
			keptLabels = append(keptLabels, allRenditions[idx].Name)
		}
		audioRenditions = filtered
		d.logger.Info("audio tracks selected",
			domain.F("episode", epLabel),
			domain.F("available", len(allRenditions)),
			domain.F("kept", len(audioRenditions)),
			domain.F("tracks", strings.Join(keptLabels, " | ")),
		)
	}

	d.logger.Info("selected quality",
		domain.F("episode", epLabel),
		domain.F("quality", selected.Label()),
		domain.F("audio_tracks", len(audioRenditions)),
		domain.F("preference", string(quality)),
	)

	// 3. Create temp directory.
	tmpDir := outPath + ".hls-tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	// 4. Fetch video media playlist.
	videoPlaylist, err := FetchMediaPlaylist(ctx, d.client, selected.URL, d.auth)
	if err != nil {
		return nil, fmt.Errorf("media playlist: %w", err)
	}
	if len(videoPlaylist.Segments) == 0 {
		return nil, fmt.Errorf("no segments found in media playlist")
	}

	// 5. Fetch audio media playlists.
	type audioJob struct {
		rendition AudioRendition
		playlist  *MediaPlaylist
		outFile   string
	}
	var audioJobs []audioJob
	for i, a := range audioRenditions {
		ap, err := FetchMediaPlaylist(ctx, d.client, a.URI, d.auth)
		if err != nil {
			d.logger.Warn("audio playlist fetch failed, skipping track",
				domain.F("episode", epLabel),
				domain.F("audio", a.Name),
				domain.F("error", err.Error()),
			)
			continue
		}
		audioJobs = append(audioJobs, audioJob{
			rendition: a,
			playlist:  ap,
			outFile:   filepath.Join(tmpDir, fmt.Sprintf("audio_%d.ts", i)),
		})
	}

	// Total segments across video + all audio for progress.
	totalSegments := len(videoPlaylist.Segments)
	for _, aj := range audioJobs {
		totalSegments += len(aj.playlist.Segments)
	}

	d.logger.Info("segment lists fetched",
		domain.F("episode", epLabel),
		domain.F("video_segments", len(videoPlaylist.Segments)),
		domain.F("audio_tracks", len(audioJobs)),
		domain.F("total_segments", totalSegments),
		domain.F("duration", fmt.Sprintf("%.0fs", videoPlaylist.TotalDuration)),
	)

	track := domain.TrackRef{Kind: domain.TrackVideo, Index: 0}

	// Per-track progress tracking for nested display. Index 0 is video,
	// followed by one entry per audio track (same order as audioJobs).
	trackInfos := make([]domain.TrackProgressInfo, 0, 1+len(audioJobs))
	trackInfos = append(trackInfos, domain.TrackProgressInfo{
		Label:         "Video",
		TotalSegments: len(videoPlaylist.Segments),
	})
	for _, aj := range audioJobs {
		label := "Audio"
		switch {
		case aj.rendition.Name != "":
			label = "Audio: " + aj.rendition.Name
		case aj.rendition.Language != "":
			label = "Audio: " + aj.rendition.Language
		}
		trackInfos = append(trackInfos, domain.TrackProgressInfo{
			Label:         label,
			TotalSegments: len(aj.playlist.Segments),
		})
	}

	// progMu guards trackInfos and serializes progress reports, since segments
	// are downloaded concurrently across tracks.
	var progMu sync.Mutex

	// recordSegLocked counts one finished segment for a track. Caller holds progMu.
	recordSegLocked := func(trackIdx int, segBytes int64) {
		ti := &trackInfos[trackIdx]
		ti.DoneSegments++
		ti.DownloadedBytes += segBytes
		if ti.DoneSegments > 0 && ti.TotalSegments > 0 {
			ti.ApproxTotalBytes = ti.DownloadedBytes / int64(ti.DoneSegments) * int64(ti.TotalSegments)
		}
	}

	// reportLocked emits a progress report covering the aggregate percent, total
	// estimated size, and the full per-track breakdown. Caller holds progMu.
	reportLocked := func() {
		if sink == nil {
			return
		}
		var (
			doneSegments int
			totalBytes   int64
			approxTotal  int64
		)
		for i := range trackInfos {
			doneSegments += trackInfos[i].DoneSegments
			totalBytes += trackInfos[i].DownloadedBytes
			approxTotal += trackInfos[i].ApproxTotalBytes
		}

		pct := 0
		if totalSegments > 0 {
			pct = doneSegments * 100 / totalSegments
		}
		sink.TrackProgress(key, track, pct)

		if hlsSink, ok := sink.(domain.HLSProgressSink); ok {
			// Send a copy so the consumer can retain it safely.
			snapshot := make([]domain.TrackProgressInfo, len(trackInfos))
			copy(snapshot, trackInfos)
			hlsSink.HLSProgress(key, snapshot)
		}
		if segSink, ok := sink.(domain.SegmentProgressSink); ok {
			segSink.SegmentProgress(key, doneSegments, totalSegments, totalBytes, approxTotal)
		} else if byteSink, ok := sink.(domain.ByteProgressSink); ok {
			byteSink.ByteProgress(key, totalBytes, approxTotal)
		}
	}

	// updateTrack records progress for a single freshly-downloaded segment and
	// emits a progress report. Safe for concurrent use.
	updateTrack := func(trackIdx int, segBytes int64) {
		progMu.Lock()
		defer progMu.Unlock()
		recordSegLocked(trackIdx, segBytes)
		reportLocked()
	}

	// Shared semaphore bounding the number of segments fetched in parallel
	// across ALL tracks. This lets audio download alongside video instead of
	// waiting for the video track to finish.
	concurrency := d.concurrency
	if concurrency < 1 {
		concurrency = defaultConcurrency
	}
	// Guarantee every track (video + each audio) can have at least one segment
	// in flight simultaneously, so audio always downloads together with video.
	if nTracks := 1 + len(audioJobs); concurrency < nTracks {
		concurrency = nTracks
	}
	sem := make(chan struct{}, concurrency)

	// downloadTrack fetches every segment of a single track into segDir (with
	// resume + bounded concurrency), then concatenates them into outPath.
	downloadTrack := func(ctx context.Context, trackIdx int, initURI string, segments []Segment, segDir, outPath string) error {
		if err := os.MkdirAll(segDir, 0755); err != nil {
			return fmt.Errorf("create segment dir: %w", err)
		}

		gctx, cancel := context.WithCancel(ctx)
		defer cancel()

		var (
			wg       sync.WaitGroup
			errMu    sync.Mutex
			firstErr error
		)
		setErr := func(err error) {
			errMu.Lock()
			if firstErr == nil {
				firstErr = err
				cancel()
			}
			errMu.Unlock()
		}

		for _, seg := range segments {
			if gctx.Err() != nil {
				break
			}
			segPath := filepath.Join(segDir, fmt.Sprintf("seg_%05d.ts", seg.Index))

			// Resume: skip already-downloaded segments SILENTLY — they were counted
			// by the pre-scan baseline before the workers started. Re-reporting them
			// here would stream multi-GB byte deltas through the live progress path
			// in under a second, which the UI's speed/ETA estimator reads as a
			// gigabytes-per-second download.
			if info, statErr := os.Stat(segPath); statErr == nil && info.Size() > 0 {
				continue
			}

			// Acquire a concurrency slot (or stop on cancellation).
			select {
			case sem <- struct{}{}:
			case <-gctx.Done():
				continue
			}
			if gctx.Err() != nil {
				<-sem // release the slot we just took
				break
			}

			wg.Add(1)
			go func(seg Segment, segPath string) {
				defer wg.Done()
				defer func() { <-sem }()

				n, err := d.downloadSegment(gctx, seg, segPath)
				if err != nil {
					os.Remove(segPath)
					setErr(fmt.Errorf("segment %d failed: %w", seg.Index, err))
					return
				}
				updateTrack(trackIdx, n)
			}(seg, segPath)
		}

		wg.Wait()

		if firstErr != nil {
			return firstErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// fMP4/CMAF tracks need the EXT-X-MAP init segment fetched and written
		// ahead of the media fragments. Resume-safe: skip if already on disk.
		initPath := ""
		if initURI != "" {
			initPath = filepath.Join(segDir, "init.mp4")
			if info, statErr := os.Stat(initPath); statErr != nil || info.Size() == 0 {
				if _, err := d.downloadSegment(ctx, Segment{URL: initURI, Index: -1}, initPath); err != nil {
					return fmt.Errorf("init segment: %w", err)
				}
			}
		}

		return d.concatenateSegmentsDir(initPath, segments, segDir, outPath)
	}

	// 6. Download all tracks (video + audio) in parallel. Segments within and
	// across tracks share the global concurrency semaphore.
	videoDir := filepath.Join(tmpDir, "video")
	videoPath := filepath.Join(tmpDir, "video.ts")
	resultAudio := make([]domain.HLSAudioTrack, len(audioJobs))

	// Resume pre-scan: count segments left by a previous attempt into the track
	// totals BEFORE the workers start, and publish them as ONE baseline report.
	// The UI's speed estimator measures byte deltas between reports, so resumed
	// data must arrive in the very first report (which only sets the baseline) —
	// trickling it through the live path would read as GB/s of "download speed".
	// Runs before the track goroutines exist, so trackInfos access is safe.
	preScan := func(trackIdx int, segments []Segment, segDir string) int {
		n := 0
		for _, seg := range segments {
			p := filepath.Join(segDir, fmt.Sprintf("seg_%05d.ts", seg.Index))
			if info, err := os.Stat(p); err == nil && info.Size() > 0 {
				recordSegLocked(trackIdx, info.Size())
				n++
			}
		}
		return n
	}
	resumed := preScan(0, videoPlaylist.Segments, videoDir)
	for ai, aj := range audioJobs {
		resumed += preScan(1+ai, aj.playlist.Segments, filepath.Join(tmpDir, fmt.Sprintf("audio_%d", ai)))
	}
	if resumed > 0 {
		progMu.Lock()
		reportLocked()
		progMu.Unlock()
		d.logger.Info("resuming from partial segments",
			domain.F("episode", epLabel),
			domain.F("resumed_segments", resumed),
		)
	}

	var (
		trackWG  sync.WaitGroup
		trackErr error
		errOnce  sync.Once
	)
	recordErr := func(err error) {
		if err != nil {
			errOnce.Do(func() { trackErr = err })
		}
	}

	// Video track (index 0).
	trackWG.Add(1)
	go func() {
		defer trackWG.Done()
		if err := downloadTrack(ctx, 0, videoPlaylist.InitURI, videoPlaylist.Segments, videoDir, videoPath); err != nil {
			recordErr(fmt.Errorf("video track: %w", err))
		}
	}()

	// Audio tracks (indices 1..N).
	for ai, aj := range audioJobs {
		trackWG.Add(1)
		go func(ai int, aj audioJob) {
			defer trackWG.Done()
			audioDir := filepath.Join(tmpDir, fmt.Sprintf("audio_%d", ai))
			if err := downloadTrack(ctx, 1+ai, aj.playlist.InitURI, aj.playlist.Segments, audioDir, aj.outFile); err != nil {
				recordErr(fmt.Errorf("audio track %d: %w", ai, err))
				return
			}
			resultAudio[ai] = domain.HLSAudioTrack{
				Path:     aj.outFile,
				Name:     aj.rendition.Name,
				Language: aj.rendition.Language,
			}
		}(ai, aj)
	}

	trackWG.Wait()

	if trackErr != nil {
		return nil, trackErr
	}

	var totalBytes int64
	for i := range trackInfos {
		totalBytes += trackInfos[i].DownloadedBytes
	}

	d.logger.Info("HLS download complete",
		domain.F("episode", epLabel),
		domain.F("quality", selected.Label()),
		domain.F("audio_tracks", len(resultAudio)),
		domain.F("concurrency", concurrency),
		domain.F("size", formatHLSBytes(totalBytes)),
	)

	codec := "h264"
	if selected.IsH265() {
		codec = "h265"
	}

	return &domain.HLSDownloadResult{
		Resolution:  selected.Resolution,
		BitrateKbps: selected.BitrateKbps(),
		Codec:       codec,
		TotalBytes:  totalBytes,
		VideoPath:   videoPath,
		AudioTracks: resultAudio,
		TempDir:     tmpDir,
	}, nil
}

// downloadSegment downloads a single segment with retries.
func (d *Downloader) downloadSegment(ctx context.Context, seg Segment, outPath string) (int64, error) {
	var lastErr error

	for attempt := 0; attempt < maxSegmentRetries; attempt++ {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}

		if attempt > 0 {
			delay := segmentRetryDelay * time.Duration(1<<(attempt-1))
			if delay > 15*time.Second {
				delay = 15 * time.Second
			}
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(delay):
			}
		}

		n, err := d.fetchSegment(ctx, seg.URL, outPath)
		if err == nil {
			return n, nil
		}

		lastErr = err
		d.logger.Debug("segment retry",
			domain.F("segment", seg.Index),
			domain.F("attempt", attempt+1),
			domain.F("error", err.Error()),
		)
	}

	return 0, fmt.Errorf("after %d attempts: %w", maxSegmentRetries, lastErr)
}

// fetchSegment downloads a single segment to disk.
func (d *Downloader) fetchSegment(ctx context.Context, segURL, outPath string) (int64, error) {
	// Per-segment timeout: 120 seconds for slow CDN connections.
	segCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(segCtx, http.MethodGet, segURL, nil)
	if err != nil {
		return 0, err
	}
	applyHLSAuth(req, d.auth)

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}

	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()

	if copyErr != nil {
		os.Remove(outPath)
		return 0, copyErr
	}
	if closeErr != nil {
		os.Remove(outPath)
		return 0, closeErr
	}

	return n, nil
}

// concatenateSegmentsDir joins all segment files from segDir into outPath. HLS
// MPEG-TS segments concatenate byte-by-byte; fMP4/CMAF segments do too, but only
// once the EXT-X-MAP init segment (ftyp+moov) is written first — initPath points
// to it (empty for plain TS streams).
func (d *Downloader) concatenateSegmentsDir(initPath string, segments []Segment, segDir, outPath string) error {
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if initPath != "" {
		f, err := os.Open(initPath)
		if err != nil {
			return fmt.Errorf("open init segment: %w", err)
		}
		_, err = io.Copy(out, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("copy init segment: %w", err)
		}
	}

	for _, seg := range segments {
		segPath := filepath.Join(segDir, fmt.Sprintf("seg_%05d.ts", seg.Index))
		f, err := os.Open(segPath)
		if err != nil {
			return fmt.Errorf("open segment %d: %w", seg.Index, err)
		}
		_, err = io.Copy(out, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("copy segment %d: %w", seg.Index, err)
		}
	}

	return nil
}

// formatHLSBytes formats bytes for logging.
func formatHLSBytes(b int64) string {
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
	default:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	}
}

// Verify that *Downloader satisfies domain.HLSDownloader at compile time.
var _ domain.HLSDownloader = (*Downloader)(nil)
