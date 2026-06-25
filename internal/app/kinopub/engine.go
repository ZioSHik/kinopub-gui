package kinopub

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
)

// engine orchestrates the download workflow using injected dependencies.
type engine struct {
	deps Dependencies

	// retryBackoff computes the wait before an episode's next attempt. When
	// nil, episodeRetryBackoff is used. It exists as a field so tests can
	// shrink the backoff; production code leaves it nil.
	retryBackoff func(attempts int) time.Duration
}

// backoffFor returns the retry backoff for the given attempt count, using the
// engine's override when present.
func (e *engine) backoffFor(attempts int) time.Duration {
	if e.retryBackoff != nil {
		return e.retryBackoff(attempts)
	}
	return episodeRetryBackoff(attempts)
}

// consecutiveFailLimit is the number of consecutive media resolution failures
// before the engine stops trying further episodes. This prevents hammering the
// CDN when links have expired or the server is blocking.
const consecutiveFailLimit = 3

// run executes the full download pipeline with parallel downloads:
// parse feed → filter → resolve + download N episodes concurrently.
// Each worker does lazy resolution immediately before downloading so CDN links
// stay fresh. MaxConcurrency controls the parallelism (default 2).
func (e *engine) run(ctx context.Context, cfg domain.RunConfig) (domain.RunResult, error) {
	if e.deps.HLSDownloader == nil || e.deps.PageScraper == nil {
		return domain.RunResult{}, fmt.Errorf("downloader not configured")
	}
	if cfg.InputURL == "" {
		return domain.RunResult{}, fmt.Errorf("a kino.pub URL is required")
	}
	return e.runHLS(ctx, cfg)
}

// runHLS implements the HLS segment-based download pipeline.
// It scrapes the page for PLAYER_PLAYLIST, selects quality, downloads via HLS segments.
func (e *engine) runHLS(ctx context.Context, cfg domain.RunConfig) (domain.RunResult, error) {
	log := e.deps.Logger.Component("engine-hls")

	// 1. Extract playlist from page, retrying a few times: kino.pub sits behind
	// Cloudflare and the first request after an idle period often fails
	// transiently (timeout / 5xx / reset). Without this a flaky first hit fails
	// the whole download before it starts.
	log.Info("extracting HLS playlist from page", domain.F("url", cfg.InputURL))
	const scrapeAttempts = 4
	var (
		playlist *domain.PagePlaylist
		err      error
	)
	for attempt := 1; attempt <= scrapeAttempts; attempt++ {
		playlist, err = e.deps.PageScraper.ExtractAllSeasons(ctx, cfg.InputURL)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return domain.RunResult{}, fmt.Errorf("page scrape: %w", ctx.Err())
		}
		if attempt < scrapeAttempts {
			delay := time.Duration(attempt) * 2 * time.Second
			log.Warn("page scrape failed, retrying",
				domain.F("attempt", attempt),
				domain.F("max_attempts", scrapeAttempts),
				domain.F("retry_in", delay.String()),
				domain.F("error", err.Error()),
			)
			select {
			case <-ctx.Done():
				return domain.RunResult{}, fmt.Errorf("page scrape: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
	}
	if err != nil {
		return domain.RunResult{}, fmt.Errorf("page scrape: %w", err)
	}

	if len(playlist.Episodes) == 0 {
		return domain.RunResult{}, fmt.Errorf("no episodes found in page playlist")
	}

	log.Info("HLS playlist extracted",
		domain.F("title", playlist.Title),
		domain.F("episodes", len(playlist.Episodes)),
		domain.F("seasons", len(playlist.Seasons)),
	)

	// 2. Build a Series from the page playlist (for state store and output layout).
	series := e.buildSeriesFromPlaylist(playlist, cfg)

	// Point state store at series directory.
	seriesDir := e.seriesDirPath(cfg.OutputPath, series)
	if ss, ok := e.deps.StateStore.(interface{ SetSeriesDir(string) }); ok {
		ss.SetSeriesDir(seriesDir)
	}

	// 3. Load state.
	state, err := e.deps.StateStore.Load(ctx, series.ID)
	if err != nil {
		return domain.RunResult{}, err
	}

	// 4. Filter episodes.
	allMatching := e.matchingEpisodes(series, cfg)
	selected := e.filterCompleted(allMatching, state, cfg)
	// A per-episode retry narrows this run to just the requested episode(s) so it
	// re-downloads only what the user clicked, not every not-yet-completed one.
	if len(cfg.RetryOnly) > 0 {
		want := make(map[[2]int]bool, len(cfg.RetryOnly))
		for _, k := range cfg.RetryOnly {
			want[[2]int{k.Season, k.Episode}] = true
		}
		narrowed := selected[:0:0]
		for _, ep := range selected {
			if want[[2]int{ep.Key.Season, ep.Key.Episode}] {
				narrowed = append(narrowed, ep)
			}
		}
		selected = narrowed
	}
	if len(selected) == 0 {
		log.Info("no episodes to download (all completed)")
		return domain.RunResult{Total: 0}, nil
	}

	alreadyCompleted := len(allMatching) - len(selected)
	log.Info("HLS download starting",
		domain.F("to_download", len(selected)),
		domain.F("already_completed", alreadyCompleted),
	)

	if cfg.DryRun {
		log.Info("dry run — listing episodes")
		for _, ep := range selected {
			log.Info(fmt.Sprintf("  S%02dE%02d %s", ep.Key.Season, ep.Key.Episode, ep.Title))
		}
		return domain.RunResult{Total: len(selected)}, nil
	}

	// 5. Persist series metadata.
	seriesMeta := domain.SeriesMetadata{
		Title:     series.Title,
		PosterURL: playlist.Poster,
		InputURL:  cfg.InputURL,
		Type:      playlist.Type,
		Genres:    playlist.Genres,
		UpdatedAt: time.Now(),
	}
	_ = e.deps.StateStore.SetMetadata(ctx, series.ID, seriesMeta)

	// 5b. Download series poster for embedding as cover art.
	var posterPath string
	if playlist.Poster != "" {
		if p, perr := e.downloadPoster(ctx, playlist.Poster, seriesDir); perr == nil {
			posterPath = p
			defer os.Remove(posterPath)
		} else {
			log.Debug("poster download failed, skipping cover art",
				domain.F("error", perr.Error()))
		}
	}

	// 6. Build manifest URL map (episode key → manifest URL).
	manifestMap := make(map[domain.EpisodeKey]string)
	for _, pe := range playlist.Episodes {
		key := domain.EpisodeKey{
			Series:  series.ID,
			Season:  pe.Season,
			Episode: pe.Episode,
		}
		manifestMap[key] = pe.ManifestURL
	}

	// 7. Resolve audio-track preference before starting the live progress
	// display. An explicit --audio preference is applied directly; otherwise,
	// when the interactive menu is enabled, probe the first episode's tracks
	// and let the user choose. The resulting preference is pushed to the HLS
	// downloader for all episodes. This runs before progress rendering so the
	// interactive prompt isn't clobbered by progress redraws.
	pref := e.resolveAudioPreference(ctx, cfg, selected, manifestMap)
	e.deps.HLSDownloader.SetAudioPreference(pref)

	// 8. Start progress reporting.
	planned := make([]domain.PlannedEpisode, 0, len(selected))
	for _, ep := range selected {
		planned = append(planned, domain.PlannedEpisode{Key: ep.Key, Title: ep.Title})
	}
	plan := domain.SeriesPlan{
		Title:              series.Title,
		PosterURL:          series.PosterURL,
		Total:              len(allMatching),
		Seasons:            countSeasons(allMatching),
		AlreadyCompleted:   alreadyCompleted,
		CompletedPerSeason: countCompletedPerSeason(allMatching, state, e.deps.StateStore),
		Planned:            planned,
	}
	e.deps.ProgressReporter.Start(plan)
	defer e.deps.ProgressReporter.Stop()

	// 9. Download episodes with deferred-retry scheduling. New episodes are
	// processed in order; an episode that fails on a transient network error
	// (timeout, EOF, connection reset, 5xx) is not dropped — it is parked in a
	// retry queue and reattempted later, interleaved between new episodes and,
	// once new episodes are exhausted, cycled with backoff until it succeeds or
	// exhausts its attempt budget. Partial segments (.hls-tmp) are preserved so
	// each reattempt resumes instead of restarting.
	// Shared run state. mu guards the counters, the outcomes slice, both work
	// queues and inFlight; episodes download in parallel, so every mutation of
	// this state must hold the lock. The progress reporter has its own internal
	// locking and is therefore called without holding mu.
	var (
		mu         sync.Mutex
		succeeded  int
		failed     int
		skipped    int
		outcomes   []domain.JobOutcome
		retryQueue []*pendingEpisode
		pausedHold []*pendingEpisode                 // episodes held aside by a per-episode pause
		epCancels  = map[string]context.CancelFunc{} // per-episode cancel for in-flight downloads
		pauseMark  = map[string]bool{}               // in-flight episodes to hold (not fail) when their attempt returns
		inFlight   int
		winding    bool // set once workers have exited; the control goroutine stops mutating
	)
	// epInfo lets the live-retry control reconstruct a pending unit for any
	// selected episode by key (after it has failed and left the queues).
	epInfo := make(map[string]struct {
		ep       domain.Episode
		manifest string
	}, len(selected))
	for _, ep := range selected {
		if mURL, ok := manifestMap[ep.Key]; ok {
			epInfo[episodeKeyStr(ep.Key)] = struct {
				ep       domain.Episode
				manifest string
			}{ep, mURL}
		}
	}

	// Build the initial work list, preserving order and skipping episodes with
	// no manifest URL.
	newQueue := make([]*pendingEpisode, 0, len(selected))
	for _, ep := range selected {
		manifestURL, ok := manifestMap[ep.Key]
		if !ok || manifestURL == "" {
			log.Warn("no manifest URL for episode, skipping",
				domain.F("episode", fmt.Sprintf("S%02dE%02d", ep.Key.Season, ep.Key.Episode)),
			)
			skipped++
			continue
		}
		newQueue = append(newQueue, &pendingEpisode{ep: ep, manifest: manifestURL})
	}

	// processOne runs a single attempt for a pending episode and routes the
	// outcome: success → completed; transient failure → re-park (or give up
	// after the attempt budget); fatal failure → mark failed. The download runs
	// outside mu so episodes proceed in parallel; the lock is taken only to
	// mutate shared counters and the retry queue.
	processOne := func(pe *pendingEpisode) {
		if ctx.Err() != nil {
			// Re-park so the post-loop sweep accounts it in the result totals.
			mu.Lock()
			retryQueue = append(retryQueue, pe)
			mu.Unlock()
			return
		}
		pe.attempts++
		epLabel := fmt.Sprintf("S%02dE%02d", pe.ep.Key.Season, pe.ep.Key.Episode)
		ks := episodeKeyStr(pe.ep.Key)

		// Give this episode its own cancelable context so a per-episode pause can
		// stop just this download while its siblings keep going.
		epCtx, epCancel := context.WithCancel(ctx)
		mu.Lock()
		epCancels[ks] = epCancel
		mu.Unlock()

		res, err := e.attemptHLSEpisode(epCtx, cfg, series, pe.ep, pe.manifest, posterPath)

		mu.Lock()
		delete(epCancels, ks)
		pausedHere := pauseMark[ks]
		delete(pauseMark, ks)
		mu.Unlock()
		epCancel() // release ctx resources

		// A per-episode pause (this episode's own ctx was canceled while the whole
		// run is still alive): hold it aside, keeping its partial segments, without
		// counting a failure or burning a retry attempt. Success still wins if the
		// download happened to finish in the same instant.
		if pausedHere && res != epSuccess && ctx.Err() == nil {
			pe.attempts--
			pe.nextAt = time.Time{}
			pe.lastErr = nil
			mu.Lock()
			pausedHold = append(pausedHold, pe)
			mu.Unlock()
			return
		}

		switch res {
		case epSuccess:
			mu.Lock()
			succeeded++
			outcomes = append(outcomes, domain.JobOutcome{Key: pe.ep.Key, Succeeded: true, Attempts: pe.attempts})
			mu.Unlock()
			e.deps.ProgressReporter.EpisodeCompleted(pe.ep.Key)

		case epRetryable:
			if pe.attempts >= maxEpisodeAttempts {
				log.Warn("giving up on episode after repeated transient failures",
					domain.F("episode", epLabel),
					domain.F("attempts", pe.attempts),
					domain.F("error", err.Error()),
				)
				// Budget exhausted — clean up the segment temp directory that
				// was preserved across retries for resume (unless the run is being
				// paused, where partial data is kept for a later resume).
				if e.deps.Paused == nil || !e.deps.Paused() {
					if outPath, pathErr := e.deps.OutputLayout.EpisodePath(cfg.OutputPath, series, pe.ep); pathErr == nil {
						os.RemoveAll(outPath + ".ts.hls-tmp")
					}
				}
				mu.Lock()
				failed++
				outcomes = append(outcomes, domain.JobOutcome{Key: pe.ep.Key, Err: err, Attempts: pe.attempts})
				mu.Unlock()
				e.deps.ProgressReporter.EpisodeFailed(pe.ep.Key, err)
				return
			}
			wait := e.backoffFor(pe.attempts)
			// Capture everything read for the log/report BEFORE re-parking: once pe
			// is back in retryQueue another worker may pick it up immediately (the
			// retry backoff is zero in tests), so touching pe past the unlock races.
			attempts := pe.attempts
			key := pe.ep.Key
			pe.lastErr = err
			pe.nextAt = time.Now().Add(wait)
			mu.Lock()
			retryQueue = append(retryQueue, pe)
			mu.Unlock()
			log.Info("episode download interrupted, will retry later",
				domain.F("episode", epLabel),
				domain.F("attempt", attempts),
				domain.F("retry_in", wait.Round(time.Second).String()),
				domain.F("error", err.Error()),
			)
			reportEpisodeDeferred(e.deps.ProgressReporter, key, err, attempts)

		case epFatal:
			if ctx.Err() != nil {
				return
			}
			log.Warn("episode failed",
				domain.F("episode", epLabel),
				domain.F("error", err.Error()),
			)
			mu.Lock()
			failed++
			outcomes = append(outcomes, domain.JobOutcome{Key: pe.ep.Key, Err: err, Attempts: pe.attempts})
			mu.Unlock()
			e.deps.ProgressReporter.EpisodeFailed(pe.ep.Key, err)
		}
	}

	// moveToFront promotes the episode matching key to the head of newQueue so it
	// is picked up next. A deferred (parked) episode is pulled out of the retry
	// queue and made immediately ready. No-op if the episode is already running,
	// finished, or unknown. Caller must hold mu.
	moveToFront := func(key domain.EpisodeKey) {
		for i, pe := range newQueue {
			if pe.ep.Key.Season == key.Season && pe.ep.Key.Episode == key.Episode {
				newQueue = append(newQueue[:i], newQueue[i+1:]...)
				newQueue = append([]*pendingEpisode{pe}, newQueue...)
				return
			}
		}
		for i, pe := range retryQueue {
			if pe.ep.Key.Season == key.Season && pe.ep.Key.Episode == key.Episode {
				retryQueue = append(retryQueue[:i], retryQueue[i+1:]...)
				pe.nextAt = time.Time{} // skip the remaining backoff
				newQueue = append([]*pendingEpisode{pe}, newQueue...)
				return
			}
		}
	}

	// drainPrioritize applies all pending "download next" requests. Caller holds mu.
	prioritize := e.deps.PrioritizeRequests
	drainPrioritize := func() {
		if prioritize == nil {
			return
		}
		for {
			select {
			case key := <-prioritize:
				moveToFront(key)
			default:
				return
			}
		}
	}

	// takeFromQueues removes the episode matching key from newQueue or retryQueue
	// and returns it, or nil if it is not waiting (already in flight / held /
	// finished). Caller holds mu.
	takeFromQueues := func(key domain.EpisodeKey) *pendingEpisode {
		for i, pe := range newQueue {
			if pe.ep.Key.Season == key.Season && pe.ep.Key.Episode == key.Episode {
				newQueue = append(newQueue[:i], newQueue[i+1:]...)
				return pe
			}
		}
		for i, pe := range retryQueue {
			if pe.ep.Key.Season == key.Season && pe.ep.Key.Episode == key.Episode {
				retryQueue = append(retryQueue[:i], retryQueue[i+1:]...)
				return pe
			}
		}
		return nil
	}

	// takeFromHold removes the held episode matching key and returns it (nil if
	// not held). Caller holds mu.
	takeFromHold := func(key domain.EpisodeKey) *pendingEpisode {
		for i, pe := range pausedHold {
			if pe.ep.Key.Season == key.Season && pe.ep.Key.Episode == key.Episode {
				pe2 := pe
				pausedHold = append(pausedHold[:i], pausedHold[i+1:]...)
				return pe2
			}
		}
		return nil
	}

	// inFlightOrQueued reports whether the episode is currently downloading,
	// waiting, or held — i.e. NOT a candidate for live re-queue. Caller holds mu.
	inFlightOrQueued := func(ks string) bool {
		if epCancels[ks] != nil {
			return true
		}
		for _, pe := range newQueue {
			if episodeKeyStr(pe.ep.Key) == ks {
				return true
			}
		}
		for _, pe := range retryQueue {
			if episodeKeyStr(pe.ep.Key) == ks {
				return true
			}
		}
		for _, pe := range pausedHold {
			if episodeKeyStr(pe.ep.Key) == ks {
				return true
			}
		}
		return false
	}

	// hasSucceeded reports whether the episode already completed in this run, so a
	// duplicate live retry can't re-download it and double-count success. Caller
	// holds mu.
	hasSucceeded := func(ks string) bool {
		for _, o := range outcomes {
			if o.Succeeded && episodeKeyStr(o.Key) == ks {
				return true
			}
		}
		return false
	}

	// applyPauseLocked holds an episode aside for a per-episode pause: a download
	// already in flight has its own context canceled (and is marked so processOne
	// parks it instead of failing it); a still-queued episode is pulled straight
	// into the hold so a freed worker can't start it. Caller must hold mu.
	pauseReq := e.deps.PauseRequests
	applyPauseLocked := func(key domain.EpisodeKey) {
		if winding {
			return
		}
		ks := episodeKeyStr(key)
		if c := epCancels[ks]; c != nil {
			pauseMark[ks] = true // processOne will hold it when the attempt returns
			c()                  // stop this episode's download now
		} else if pe := takeFromQueues(key); pe != nil {
			pausedHold = append(pausedHold, pe)
		}
	}

	// drainPause applies every pause request that has already been delivered.
	// Calling it in takeTask before dispatch means a pause the user has already
	// sent is honored before a freed worker can start that episode — so a queued,
	// paused episode is never even handed to the downloader. The live-control
	// goroutine still handles pauses that arrive mid-download. Caller holds mu.
	drainPause := func() {
		if pauseReq == nil {
			return
		}
		for {
			select {
			case key := <-pauseReq:
				applyPauseLocked(key)
			default:
				return
			}
		}
	}

	// takeTask picks the next runnable episode under the lock. It returns
	// (task, wait, done): a task to run now; or no task with a wait hint (sleep
	// then re-poll — e.g. a retry still inside its backoff window, or other
	// episodes are mid-flight and may re-park work); or done=true when both
	// queues are empty and nothing is in flight, so the worker may exit.
	takeTask := func() (*pendingEpisode, time.Duration, bool) {
		mu.Lock()
		defer mu.Unlock()
		drainPrioritize()
		drainPause()
		if len(newQueue) > 0 {
			pe := newQueue[0]
			newQueue = newQueue[1:]
			inFlight++
			return pe, 0, false
		}
		if len(retryQueue) > 0 {
			idx := earliestDeferredIndex(retryQueue)
			pe := retryQueue[idx]
			if wait := time.Until(pe.nextAt); wait > 0 {
				if wait > time.Second {
					wait = time.Second // cap so cancellation stays responsive
				}
				return nil, wait, false
			}
			retryQueue = append(retryQueue[:idx], retryQueue[idx+1:]...)
			inFlight++
			return pe, 0, false
		}
		if inFlight > 0 {
			return nil, 200 * time.Millisecond, false
		}
		// Nothing runnable, but episodes are paused: keep the run alive so they can
		// be resumed. Poll periodically (and stay responsive to a resume/cancel).
		if len(pausedHold) > 0 {
			return nil, 500 * time.Millisecond, false
		}
		return nil, 0, true
	}

	// Live-control goroutine: applies pause / resume / retry requests promptly,
	// even mid-download (a pause cancels the episode's own context; a resume or
	// retry re-queues it). It runs alongside the workers and stops when the run
	// ends. Requests for episodes that aren't applicable are no-ops.
	resumeReq := e.deps.ResumeRequests
	retryReq := e.deps.RetryRequests
	stopCtrl := make(chan struct{})
	ctrlDone := make(chan struct{})
	go func() {
		defer close(ctrlDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCtrl:
				return
			case key := <-pauseReq:
				mu.Lock()
				applyPauseLocked(key)
				mu.Unlock()
			case key := <-resumeReq:
				ks := episodeKeyStr(key)
				mu.Lock()
				if !winding {
					delete(pauseMark, ks)
					if pe := takeFromHold(key); pe != nil {
						pe.nextAt = time.Time{}
						newQueue = append([]*pendingEpisode{pe}, newQueue...)
					}
				}
				mu.Unlock()
			case key := <-retryReq:
				ks := episodeKeyStr(key)
				mu.Lock()
				// Skip when the run is winding down (no worker would pick it up — it
				// would otherwise be re-queued only to be swept as failed), when the
				// episode is already active/queued/held, or when it already SUCCEEDED
				// (a duplicate retry must not re-download and double-count it).
				if !winding && !inFlightOrQueued(ks) && !hasSucceeded(ks) {
					if info, ok := epInfo[ks]; ok {
						// Undo the prior failure tally so totals stay correct.
						for i, o := range outcomes {
							if episodeKeyStr(o.Key) == ks && !o.Succeeded {
								outcomes = append(outcomes[:i], outcomes[i+1:]...)
								if failed > 0 {
									failed--
								}
								break
							}
						}
						newQueue = append([]*pendingEpisode{{ep: info.ep, manifest: info.manifest}}, newQueue...)
					}
				}
				mu.Unlock()
			}
		}
	}()

	// Download episodes in parallel: cfg.MaxConcurrency episodes at a time.
	// Segment concurrency within each episode is bounded separately by the HLS
	// downloader, so the two budgets don't multiply into a CDN flood.
	workers := cfg.MaxConcurrency
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				pe, wait, done := takeTask()
				if done {
					return
				}
				if pe == nil {
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
					continue
				}
				processOne(pe)
				mu.Lock()
				inFlight--
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Workers have exited; mark the run as winding down so the control goroutine
	// stops accepting late pause/resume/retry requests (they'd otherwise mutate
	// the queues with no worker left to act on them). Then stop it and wait for it
	// to exit before the sweep, so it can't race the final tally.
	mu.Lock()
	winding = true
	mu.Unlock()
	close(stopCtrl)
	<-ctrlDone

	// When the run is being paused (not canceled), keep partial segment data so a
	// later resume continues from where it stopped instead of restarting.
	jobPaused := e.deps.Paused != nil && e.deps.Paused()

	// On a PAUSE, every not-yet-finished episode is preserved for resume — it is
	// pending, not failed. Counting it as a failure would make a paused job's
	// summary read "N failed" while its rows correctly read "paused", and those
	// episodes succeed on resume. So when paused we keep their partial data and
	// leave them OUT of the failed tally/outcomes; on a real cancel they count as
	// failures and their temp segments are cleaned up.
	sweep := func(q []*pendingEpisode) {
		for _, pe := range q {
			if jobPaused {
				continue // preserved-for-resume: not a failure, keep .hls-tmp
			}
			failed++
			err := pe.lastErr
			if err == nil {
				err = ctx.Err()
			}
			outcomes = append(outcomes, domain.JobOutcome{Key: pe.ep.Key, Err: err, Attempts: pe.attempts})
			if outPath, pathErr := e.deps.OutputLayout.EpisodePath(cfg.OutputPath, series, pe.ep); pathErr == nil {
				os.RemoveAll(outPath + ".ts.hls-tmp")
			}
		}
	}
	sweep(retryQueue) // in-flight episodes re-parked here on stop
	sweep(newQueue)   // never-started episodes
	sweep(pausedHold) // episodes held aside by a per-episode pause

	result := domain.RunResult{
		Total:     len(selected),
		Succeeded: succeeded,
		Failed:    failed,
		Skipped:   skipped,
		Outcomes:  outcomes,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

// episodeKeyStr formats an episode key as "S{season}E{episode}" for use as a map
// key in the live-control state (matches the season+episode identity used by the
// pause/resume/retry requests).
func episodeKeyStr(k domain.EpisodeKey) string {
	return fmt.Sprintf("S%dE%d", k.Season, k.Episode)
}

// maxEpisodeAttempts bounds how many times the engine reattempts a single
// episode that keeps failing on transient network errors before giving up. The
// per-segment retry budget is separate and applies within each attempt.
const maxEpisodeAttempts = 8

// pendingEpisode is a unit of work in the deferred-retry scheduler.
type pendingEpisode struct {
	ep       domain.Episode
	manifest string
	attempts int       // attempts made so far
	nextAt   time.Time // earliest time the next attempt may run (backoff)
	lastErr  error     // error from the most recent attempt
}

// episodeOutcome classifies the result of a single download+mux attempt.
type episodeOutcome int

const (
	epSuccess   episodeOutcome = iota // downloaded and muxed
	epRetryable                       // transient failure — worth retrying later
	epFatal                           // permanent failure — do not retry
)

// episodeRetryBackoff returns how long to wait before the next attempt of an
// episode that has failed `attempts` times. It grows linearly and is capped so
// a stuck CDN segment doesn't stall the whole run indefinitely.
func episodeRetryBackoff(attempts int) time.Duration {
	wait := time.Duration(attempts) * 20 * time.Second
	if wait > 3*time.Minute {
		wait = 3 * time.Minute
	}
	return wait
}

// readyDeferredIndex returns the index of a deferred episode whose backoff
// window has elapsed (earliest-due first), or -1 if none are ready.
func readyDeferredIndex(q []*pendingEpisode, now time.Time) int {
	best := -1
	for i, pe := range q {
		if pe.nextAt.After(now) {
			continue
		}
		if best == -1 || pe.nextAt.Before(q[best].nextAt) {
			best = i
		}
	}
	return best
}

// earliestDeferredIndex returns the index of the deferred episode due soonest.
// The queue must be non-empty.
func earliestDeferredIndex(q []*pendingEpisode) int {
	best := 0
	for i, pe := range q {
		if pe.nextAt.Before(q[best].nextAt) {
			best = i
		}
	}
	return best
}

// reportEpisodeDeferred notifies the progress reporter that an episode is
// parked for a later retry, if the reporter supports it. Reporters without the
// optional hook simply don't show a distinct "deferred" state.
func reportEpisodeDeferred(r domain.ProgressReporter, key domain.EpisodeKey, err error, attempts int) {
	if d, ok := r.(interface {
		EpisodeDeferred(domain.EpisodeKey, error, int)
	}); ok {
		d.EpisodeDeferred(key, err, attempts)
	}
}

// attemptHLSEpisode performs one full download+mux attempt for a single
// episode. It returns epSuccess on completion, epRetryable for transient
// network failures (the .hls-tmp segments are left in place so the next
// attempt resumes), or epFatal for permanent errors. The returned error is
// non-nil for the two failure outcomes.
func (e *engine) attemptHLSEpisode(
	ctx context.Context,
	cfg domain.RunConfig,
	series domain.Series,
	ep domain.Episode,
	manifestURL string,
	posterPath string,
) (episodeOutcome, error) {
	log := e.deps.Logger.Component("engine-hls")
	epLabel := fmt.Sprintf("S%02dE%02d", ep.Key.Season, ep.Key.Episode)

	outPath, err := e.deps.OutputLayout.EpisodePath(cfg.OutputPath, series, ep)
	if err != nil {
		return epFatal, fmt.Errorf("output path: %w", err)
	}
	if err := e.deps.OutputLayout.EnsureDirs(outPath); err != nil {
		return epFatal, fmt.Errorf("create directory: %w", err)
	}

	e.deps.ProgressReporter.EpisodeStarted(ep.Key)

	tsPath := outPath + ".ts"
	hlsResult, dlErr := e.deps.HLSDownloader.DownloadEpisode(ctx, manifestURL, cfg.Quality, tsPath, ep.Key, e.deps.ProgressReporter)
	if dlErr != nil {
		if ctx.Err() != nil {
			return epRetryable, dlErr
		}
		if isTransientDownloadError(dlErr) {
			return epRetryable, dlErr
		}
		// Terminal failure: clean up the segment temp directory so it does not
		// accumulate on disk. Retryable paths deliberately leave it in place so
		// the next attempt can resume from already-downloaded segments.
		os.RemoveAll(tsPath + ".hls-tmp")
		return epFatal, dlErr
	}

	// Mux downloaded video + audio streams into the final container.
	log.Info("muxing",
		domain.F("episode", epLabel),
		domain.F("quality", fmt.Sprintf("%s @ %d kbps", hlsResult.Resolution, hlsResult.BitrateKbps)),
		domain.F("audio_tracks", len(hlsResult.AudioTracks)),
	)

	muxJob := domain.Job{
		Episode:     ep,
		OutPath:     outPath,
		PosterPath:  posterPath,
		SeriesTitle: series.Title,
	}

	var remuxErr error
	if muxer, ok := e.deps.Downloader.(domain.HLSMuxer); ok {
		remuxErr = muxer.MuxHLS(ctx, muxJob, hlsResult)
	} else {
		remuxErr = fmt.Errorf("downloader does not support HLS muxing")
	}

	// Clean up temp segment files regardless of mux outcome.
	if hlsResult.TempDir != "" {
		os.RemoveAll(hlsResult.TempDir)
	}

	if remuxErr != nil {
		log.Warn("remux failed",
			domain.F("episode", epLabel),
			domain.F("error", remuxErr.Error()),
		)
		return epFatal, remuxErr
	}

	// Mark completed.
	info, _ := os.Stat(outPath)
	var fileSize int64
	if info != nil {
		fileSize = info.Size()
	}
	completedInfo := domain.CompletedInfo{
		Key:        ep.Key,
		Path:       outPath,
		Bytes:      fileSize,
		Title:      ep.Title,
		Quality:    fmt.Sprintf("%s/%s", hlsResult.Resolution, hlsResult.Codec),
		Resolution: hlsResult.Resolution,
		BitRate:    hlsResult.BitrateKbps,
		PageLink:   ep.PageLink,
		MediaURL:   manifestURL,
	}
	_ = e.deps.StateStore.MarkCompleted(ctx, completedInfo)

	return epSuccess, nil
}

// transientErrorMarkers are substrings identifying recoverable network/CDN
// failures: the connection or server hiccupped but is likely to recover, so
// the episode should be retried later rather than abandoned.
var transientErrorMarkers = []string{
	"context deadline exceeded", // per-segment timeout
	"deadline exceeded",
	"timeout",
	"timed out",
	"unexpected eof",
	"connection reset",
	"connection refused",
	"broken pipe",
	"eof",
	"no such host",      // transient DNS
	"temporary failure", // transient DNS
	"tls handshake",
	"http 429", "429", // rate limited
	"http 500", "500",
	"http 502", "502",
	"http 503", "503",
	"http 504", "504",
	"server misbehaving",
}

// isTransientDownloadError reports whether err looks like a recoverable
// network/CDN condition (as opposed to a permanent error such as a bad
// manifest or a 404). The check is substring-based because the underlying
// errors are wrapped and stringified across several layers.
func isTransientDownloadError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range transientErrorMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// Precedence:
//  1. An explicit cfg.AudioPref (from the --audio flag) is used as-is, with
//     Prefer hints enriched from the first episode's tracks so a missing dub
//     falls back within the desired language.
//  2. Otherwise, if the interactive menu is enabled and a chooser is wired,
//     probe the first episode's tracks and prompt the user. The chosen tracks
//     are generalized into a cross-episode preference.
//  3. Otherwise, keep all tracks (zero preference).
func (e *engine) resolveAudioPreference(
	ctx context.Context,
	cfg domain.RunConfig,
	selected []domain.Episode,
	manifestMap map[domain.EpisodeKey]string,
) domain.AudioPreference {
	log := e.deps.Logger.Component("engine")

	// Fast path: no explicit preference and no interactive menu → keep all
	// tracks without probing the network.
	menuActive := cfg.AudioMenu && e.deps.AudioChooser != nil
	if cfg.AudioPref.IsAll() && !menuActive {
		return domain.AudioPreference{}
	}

	// Probe the first episode's audio tracks (best-effort) — used for both the
	// menu and for enriching Prefer hints.
	var tracks []domain.AudioTrackInfo
	if len(selected) > 0 {
		if url, ok := manifestMap[selected[0].Key]; ok && url != "" {
			if t, err := e.deps.HLSDownloader.ListAudioTracks(ctx, url, cfg.Quality); err != nil {
				log.Debug("audio track probe failed", domain.F("error", err.Error()))
			} else {
				tracks = t
			}
		}
	}

	// 1. Explicit --audio preference.
	if !cfg.AudioPref.IsAll() {
		pref := cfg.AudioPref
		if len(pref.Prefer) == 0 && len(tracks) > 0 {
			pref.Prefer = domain.DeriveAudioPrefer(tracks, pref.Include)
		}
		log.Info("audio preference (explicit)",
			domain.F("include", strings.Join(pref.Include, ", ")),
			domain.F("exclude", strings.Join(pref.Exclude, ", ")),
		)
		return pref
	}

	// 2. Interactive menu.
	if menuActive && len(tracks) > 1 {
		chosen, err := e.deps.AudioChooser.ChooseAudio(tracks, cfg.AudioMenuTimeout)
		if err != nil {
			log.Warn("audio menu failed, keeping all tracks", domain.F("error", err.Error()))
			return domain.AudioPreference{}
		}
		if len(chosen) == 0 {
			return domain.AudioPreference{}
		}
		pref := domain.BuildAudioPreference(tracks, chosen)
		log.Info("audio preference (interactive)",
			domain.F("include", strings.Join(pref.Include, ", ")),
			domain.F("selected", len(chosen)),
		)
		return pref
	}

	// 3. Keep everything.
	return domain.AudioPreference{}
}

// buildSeriesFromPlaylist constructs a domain.Series from page playlist data.
func (e *engine) buildSeriesFromPlaylist(playlist *domain.PagePlaylist, cfg domain.RunConfig) domain.Series {
	series := domain.Series{
		ID:        domain.SeriesID(fmt.Sprintf("%d", playlist.ItemID)),
		Title:     playlist.Title,
		PosterURL: playlist.Poster,
	}

	// Group episodes by season.
	seasonMap := make(map[int][]domain.Episode)
	for _, pe := range playlist.Episodes {
		ep := domain.Episode{
			Key: domain.EpisodeKey{
				Series:  series.ID,
				Season:  pe.Season,
				Episode: pe.Episode,
			},
			Title:    pe.EpisodeTitle,
			Duration: time.Duration(pe.Duration) * time.Second,
			MediaSources: []domain.MediaSource{{
				Kind: domain.MediaHLS,
				URL:  pe.ManifestURL,
			}},
		}
		seasonMap[pe.Season] = append(seasonMap[pe.Season], ep)
	}

	// Build sorted seasons.
	seasonNums := make([]int, 0, len(seasonMap))
	for n := range seasonMap {
		seasonNums = append(seasonNums, n)
	}
	sort.Ints(seasonNums)

	for _, sn := range seasonNums {
		eps := seasonMap[sn]
		sort.Slice(eps, func(i, j int) bool {
			return eps[i].Key.Episode < eps[j].Key.Episode
		})
		series.Seasons = append(series.Seasons, domain.Season{
			Number:   sn,
			Episodes: eps,
		})
	}

	return series
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// matchingEpisodes returns all episodes matching season/episode selection (ignoring completion state).
func (e *engine) matchingEpisodes(series domain.Series, cfg domain.RunConfig) []domain.Episode {
	// An explicit per-episode allow-list (from the GUI picker) wins over the
	// season/episode cross-product so an exact selection is honored.
	if len(cfg.SelectedEpisodes) > 0 {
		want := make(map[[2]int]bool, len(cfg.SelectedEpisodes))
		for _, k := range cfg.SelectedEpisodes {
			want[[2]int{k.Season, k.Episode}] = true
		}
		var matched []domain.Episode
		for _, season := range series.Seasons {
			for _, ep := range season.Episodes {
				if want[[2]int{ep.Key.Season, ep.Key.Episode}] {
					matched = append(matched, ep)
				}
			}
		}
		return matched
	}

	var matched []domain.Episode
	for _, season := range series.Seasons {
		if !cfg.SeasonSel.Matches(season.Number) {
			continue
		}
		for _, ep := range season.Episodes {
			if !cfg.EpisodeSel.Matches(ep.Key.Episode) {
				continue
			}
			matched = append(matched, ep)
		}
	}
	return matched
}

// filterCompleted removes already-completed episodes from the list (unless ForceRedownload).
func (e *engine) filterCompleted(episodes []domain.Episode, state domain.DownloadState, cfg domain.RunConfig) []domain.Episode {
	if cfg.ForceRedownload {
		return episodes
	}
	var selected []domain.Episode
	for _, ep := range episodes {
		if !e.deps.StateStore.IsCompleted(state, ep.Key) {
			selected = append(selected, ep)
		}
	}
	return selected
}

// selectEpisodes filters episodes by season/episode selection and completion state.
func (e *engine) selectEpisodes(series domain.Series, state domain.DownloadState, cfg domain.RunConfig) []domain.Episode {
	return e.filterCompleted(e.matchingEpisodes(series, cfg), state, cfg)
}

// countSeasons counts episodes per season for the progress plan.
func countSeasons(episodes []domain.Episode) map[int]int {
	m := make(map[int]int)
	for _, ep := range episodes {
		m[ep.Key.Season]++
	}
	return m
}

// countCompletedPerSeason counts how many episodes per season are already completed.
func countCompletedPerSeason(allEpisodes []domain.Episode, state domain.DownloadState, store domain.StateStore) map[int]int {
	m := make(map[int]int)
	for _, ep := range allEpisodes {
		if store.IsCompleted(state, ep.Key) {
			m[ep.Key.Season]++
		}
	}
	return m
}

// downloadExecutor adapts the Downloader interface to the JobExecutor interface
// expected by the Scheduler. Kept for compatibility.
type downloadExecutor struct {
	downloader domain.Downloader
	reporter   domain.ProgressReporter
}

// Execute implements domain.JobExecutor.
func (d *downloadExecutor) Execute(ctx context.Context, job domain.Job) error {
	d.reporter.EpisodeStarted(job.Episode.Key)
	return d.downloader.Download(ctx, job, d.reporter)
}

// seriesDirPath computes the series download directory path using the same
// sanitization logic as OutputLayout. This is used to place the state file
// inside the series folder.
func (e *engine) seriesDirPath(root string, series domain.Series) string {
	fallback := fmt.Sprintf("series_%s", string(series.ID))
	seriesDir := fsutil.SanitizeComponent(series.Title, fallback)
	return filepath.Join(root, seriesDir)
}

// downloadPoster downloads the series poster image to a temporary file in outputDir.
// Returns the path to the downloaded file, or an error if the download fails.
// The caller is responsible for removing the file when done.
func (e *engine) downloadPoster(ctx context.Context, posterURL, outputDir string) (string, error) {
	client := e.deps.ProxyProvider.HTTPClient()

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, posterURL, nil)
	if err != nil {
		return "", fmt.Errorf("poster request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("poster download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poster download: HTTP %d", resp.StatusCode)
	}

	// Write to a temp file in the output directory.
	posterPath := filepath.Join(outputDir, ".poster-cover.jpg")
	f, err := os.Create(posterPath)
	if err != nil {
		return "", fmt.Errorf("poster create file: %w", err)
	}

	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(posterPath)
		return "", fmt.Errorf("poster write: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(posterPath)
		return "", fmt.Errorf("poster close: %w", closeErr)
	}

	return posterPath, nil
}
