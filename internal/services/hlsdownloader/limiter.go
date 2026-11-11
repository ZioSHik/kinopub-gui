package hlsdownloader

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

const (
	// maxSegmentConcurrency is where the ceiling STARTS, not where it ends. It is
	// the level past which extra sockets usually stop buying throughput and start
	// making us look like a scraper — a good default, and wrong for a link fast
	// enough that the CDN's per-connection shaping, not the pipe, is the limit.
	maxSegmentConcurrency = 16

	// hardSegmentConcurrency is the real ceiling: the controller may lift its own
	// cap this far, but only while each lift is paid for by measured throughput.
	// It exists so a pathological measurement can't ramp us into a socket storm.
	hardSegmentConcurrency = 48

	// ceilingStep is how much headroom one proven lift buys. Small enough that a
	// wrong lift costs one window to detect and undo.
	ceilingStep = 4

	// probeWindow is one measurement window. It has to be long enough to average
	// over whole segments (which take seconds) yet short enough that ramping from
	// the starting limit to a good one costs a small fraction of an episode.
	probeWindow = 4 * time.Second

	// growThreshold / shrinkThreshold are the relative rate changes that count as
	// "that helped" and "that hurt". The dead band between them is what stops the
	// limit from oscillating on ordinary CDN jitter.
	growThreshold   = 1.08
	shrinkThreshold = 0.92

	// settleFor is how long the controller holds a limit once it has settled,
	// before probing again. Re-probing matters because the link changes underfoot
	// — Wi-Fi to Ethernet, a video call ending, another download finishing.
	settleFor = 30 * time.Second

	// acquirePoll is a safety net: a waiter re-checks its condition this often
	// even if it misses a wake-up signal. It only bounds worst-case latency
	// before a raised limit is noticed, which is irrelevant next to the seconds a
	// segment takes.
	acquirePoll = 50 * time.Millisecond
)

// adaptiveLimiter bounds how many segments are fetched at once and tunes that
// bound at runtime by hill-climbing on measured throughput.
//
// Why this is not a constant: the right number depends on the link, and the
// spread is enormous — on fast fibre a handful of connections leaves the pipe
// half empty (the CDN shapes per connection, and one TCP stream is limited by
// RTT and window size), while on a slow or lossy link that same number causes
// contention and timeouts. Any fixed value is wrong for most users.
//
// The controller runs one measurement window at a time: it records the bytes
// finished in the window, converts that to a rate, and compares it with the
// previous window. Better means the last step up helped, so it steps up again;
// worse means we overshot (or the link degraded), so it steps back and settles;
// roughly equal means we are at the knee of the curve, so it settles there. A
// settled limit is re-probed later, because the link does not hold still.
//
// A 429 or 503 short-circuits all of that: the server has told us we are over
// the line, so the limit is halved immediately and every worker is parked for
// the Retry-After the server asked for. That is the honest version of the fixed
// "min interval between requests" knob this replaces — it costs nothing when the
// CDN is happy and reacts properly when it is not.
type adaptiveLimiter struct {
	logger domain.Logger
	now    func() time.Time // injectable so the controller is testable without sleeping

	mu       sync.Mutex
	limit    int // segments allowed in flight right now
	inFlight int
	min      int // track floor, capped by the CDN policy when enabled

	// ceiling is the controller's own cap, and it moves: when the limit is pinned
	// against it and one more window still shows more throughput, the pipe is
	// telling us the cap — not the link — is what we are measuring, so the cap
	// lifts. A 429 drops it straight back to the default. hardCeiling is the one
	// number that never moves.
	ceiling     int
	hardCeiling int

	// baseMin is the floor the limiter was built with; trackFloor is the sum of
	// the tracks of the episodes currently downloading through it. min is the
	// larger of the two, so episodes joining and leaving a shared limiter raise
	// and lower the guaranteed floor without ever dropping it below the baseline.
	baseMin            int
	trackFloor         int
	trackFloorCap      int // GUI policy: extra audio tracks must not force more sockets
	serializeDownloads bool
	episodeActive      bool
	playbackRequests   int
	playbackLeases     map[string]time.Time

	// Current measurement window.
	windowStart time.Time
	windowBytes int64
	windowDone  int

	lastRate     float64   // rate of the previous completed window (0 = none yet)
	settledAt    time.Time // hold the limit until this instant, then probe again
	coolUntil    time.Time // no new segment may start before this (429 back-pressure)
	sessionUntil time.Time // account session limit: pause new CDN requests together
	reprobeLimit int
	reprobeRate  float64

	peak int // highest limit reached, for the closing log line

	// wake is signalled (non-blocking) whenever a slot frees up or the limit
	// grows, so waiters don't have to rely on the poll interval.
	wake chan struct{}
}

// newAdaptiveLimiter returns a limiter starting at `start` slots, never going
// below `min` (which must leave every track able to have one segment in flight,
// so audio keeps downloading alongside video) and never above maxSegmentConcurrency.
func newAdaptiveLimiter(start, min int, logger domain.Logger) *adaptiveLimiter {
	if min < 1 {
		min = 1
	}
	if min > maxSegmentConcurrency {
		min = maxSegmentConcurrency
	}
	if start < min {
		start = min
	}
	if start > maxSegmentConcurrency {
		start = maxSegmentConcurrency
	}
	return &adaptiveLimiter{
		logger:      logger,
		now:         time.Now,
		limit:       start,
		min:         min,
		baseMin:     min,
		ceiling:     maxSegmentConcurrency,
		hardCeiling: hardSegmentConcurrency,
		peak:        start,
		wake:        make(chan struct{}, 1),
	}
}

// Limiter is the throughput controller in its exported form. One instance is
// meant to be shared by every Downloader in the process: it measures one link
// and one CDN, so giving each download its own would have them hill-climb
// against each other over the same bottleneck and multiply the socket budget
// nobody agreed to.
type Limiter = adaptiveLimiter

// NewLimiter builds a shared controller starting at `start` segments in flight
// (values < 1 fall back to the default). Pass it to every Downloader via
// WithLimiter.
func NewLimiter(start int, logger domain.Logger) *Limiter {
	if start < 1 {
		start = defaultConcurrency
	}
	return newAdaptiveLimiter(start, 1, logger)
}

// NewCDNLimiter separates episode admission from measured segment throughput.
// One episode uses the network; another worker may mux a completed episode.
func NewCDNLimiter(logger domain.Logger) *Limiter {
	l := NewLimiter(0, logger)
	l.trackFloorCap = 2
	l.serializeDownloads = true
	return l
}

// NewLimiterWithCeiling keeps the shared request budget below an explicit cap,
// including when the throughput controller or additional tracks request more.
func NewLimiterWithCeiling(start, ceiling int, logger domain.Logger) *Limiter {
	if ceiling < 1 {
		ceiling = 1
	}
	if ceiling > hardSegmentConcurrency {
		ceiling = hardSegmentConcurrency
	}
	l := NewLimiter(start, logger)
	l.ceiling, l.hardCeiling = ceiling, ceiling
	if l.limit > ceiling {
		l.limit = ceiling
	}
	l.peak = l.limit
	return l
}

// Stats is a snapshot of the controller, for callers deciding whether the link
// has room for more work than it is currently being given.
type Stats struct {
	Limit     int  // segments allowed in flight right now
	InFlight  int  // segments actually moving
	Ceiling   int  // the controller's current self-imposed cap
	Throttled bool // background requests are parked by CDN cooldown or playback
}

// Stats reports the live state of the controller. Safe for concurrent use.
func (l *adaptiveLimiter) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Stats{
		Limit:     l.limit,
		InFlight:  l.inFlight,
		Ceiling:   l.capLocked(),
		Throttled: l.now().Before(l.coolUntil) || l.now().Before(l.sessionUntil) || l.playbackActiveLocked(),
	}
}

// Account session exhaustion needs time for an existing playback session to
// expire. Opening the next episode immediately would consume another session.
func (l *adaptiveLimiter) sessionLimited(retryAfter time.Duration) {
	if retryAfter < time.Minute {
		retryAfter = time.Minute
	}
	l.mu.Lock()
	until := l.now().Add(retryAfter)
	if until.After(l.sessionUntil) {
		l.sessionUntil = until
	}
	// Do not resume at the concurrency that just exhausted the account. The
	// ceiling stays adaptive: successful measurements can earn the speed back.
	l.trackFloorCap = 2
	l.applyFloorLocked()
	l.limit = min(2, l.hardCeiling)
	l.ceiling = min(maxSegmentConcurrency, l.hardCeiling)
	l.lastRate, l.reprobeRate, l.reprobeLimit = 0, 0, 0
	l.settledAt = l.sessionUntil
	l.windowStart, l.windowBytes, l.windowDone = time.Time{}, 0, 0
	l.mu.Unlock()
	l.signal()
}

func (l *adaptiveLimiter) waitSession(ctx context.Context) error {
	// Already admitted requests may finish while playback starts. Waiting on a
	// player lease here would hold the very slots that the player needs.
	return l.waitReady(ctx, true)
}

// addTracks registers an episode's tracks with a shared limiter. The generic
// policy grants one slot per track; the GUI caps that floor at two so selecting
// many voiceovers does not override measured concurrency or CDN back-pressure.
// Paired with removeTracks when the episode finishes.
func (l *adaptiveLimiter) addTracks(n int) {
	if n < 1 {
		return
	}
	l.mu.Lock()
	l.trackFloor += n
	l.applyFloorLocked()
	l.mu.Unlock()
	l.signal()
}

// removeTracks drops an episode's tracks from the floor once it has finished.
// The limit itself is left where the controller put it — the throughput it was
// measured against does not change just because one episode ended.
func (l *adaptiveLimiter) removeTracks(n int) {
	if n < 1 {
		return
	}
	l.mu.Lock()
	if l.trackFloor -= n; l.trackFloor < 0 {
		l.trackFloor = 0
	}
	l.applyFloorLocked()
	l.mu.Unlock()
}

// applyFloorLocked recomputes min from the baseline and the active track count,
// raising the current limit if it now sits under the floor. Caller holds mu.
func (l *adaptiveLimiter) applyFloorLocked() {
	min := l.baseMin
	if l.trackFloor > min {
		min = l.trackFloor
	}
	if l.trackFloorCap > 0 && min > l.trackFloorCap {
		min = l.trackFloorCap
	}
	if min < 1 {
		min = 1
	}
	// The floor is a correctness guarantee (every track keeps a slot, so audio
	// never starves) and may exceed the throughput-policy ceiling — capLocked
	// resolves the two. It is deliberately NOT written into l.ceiling: that cap
	// is reserved for lifts proven by measured throughput, and a burst of
	// multi-track episodes must not leave behind headroom a later lone download
	// never earned. The hard ceiling still wins over everything.
	if min > l.hardCeiling {
		min = l.hardCeiling
	}
	l.min = min
	if l.limit < min {
		l.setLimitLocked(min)
	}
	// Tracks leaving can lower the effective cap below the current limit; give
	// the floor-granted headroom back.
	if cap := l.capLocked(); l.limit > cap {
		l.setLimitLocked(cap)
	}
}

// capLocked is the effective cap on the limit: the throughput-proven ceiling,
// or the track floor when correctness demands more than policy allows. Caller
// holds mu.
func (l *adaptiveLimiter) capLocked() int {
	if l.min > l.ceiling {
		return l.min
	}
	return l.ceiling
}

// acquire blocks until a slot is free and any throttle cool-down has expired.
// It returns ctx.Err() if the context is cancelled while waiting.
func (l *adaptiveLimiter) acquire(ctx context.Context) error {
	return l.acquireFor(ctx, false)
}

func (l *adaptiveLimiter) acquireFor(ctx context.Context, playback bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		l.mu.Lock()
		cool := max(l.coolUntil.Sub(l.now()), l.sessionUntil.Sub(l.now()))
		if cool <= 0 && (playback || !l.playbackActiveLocked()) && l.inFlight < l.limit {
			// A fresh measurement window opens when the pipe wakes from idle. A
			// window left spanning the gap (an episode's ffmpeg remux, the space
			// between jobs — the limiter is shared process-wide) would close over
			// the idle seconds, read as a throughput collapse, and shrink the
			// limit at every episode boundary for nothing.
			if l.inFlight == 0 || l.windowStart.IsZero() {
				l.windowStart, l.windowBytes, l.windowDone = l.now(), 0, 0
			}
			l.inFlight++
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		// During a cool-down there is nothing to race for, so wait it out in one
		// go; otherwise poll, in case a limit raise arrived without a signal.
		delay := acquirePoll
		if cool > 0 {
			delay = cool
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.wake:
		case <-time.After(delay):
		}
	}
}

// release frees a slot without recording a measurement — for attempts that
// ended in failure or cancellation, whose bytes would skew the rate downwards
// for reasons that have nothing to do with the concurrency level.
func (l *adaptiveLimiter) release() {
	l.mu.Lock()
	l.inFlight--
	l.mu.Unlock()
	l.signal()
}

// done frees a slot and folds a successful segment of n bytes into the current
// measurement window, advancing the controller when the window closes.
func (l *adaptiveLimiter) done(n int64) {
	l.mu.Lock()
	l.inFlight--
	if l.playbackActiveLocked() || l.now().Before(l.sessionUntil) || l.now().Before(l.coolUntil) {
		l.windowStart, l.windowBytes, l.windowDone = time.Time{}, 0, 0
		l.mu.Unlock()
		l.signal()
		return
	}
	l.windowBytes += n
	l.windowDone++

	now := l.now()
	if !l.windowStart.IsZero() {
		if elapsed := now.Sub(l.windowStart); elapsed >= probeWindow && l.windowDone > 0 {
			l.advanceLocked(float64(l.windowBytes)/elapsed.Seconds(), now)
			l.windowStart, l.windowBytes, l.windowDone = now, 0, 0
		}
	}
	l.mu.Unlock()
	l.signal()
}

// throttle applies CDN back-pressure: it parks every worker for retryAfter (the
// server's own hint) and halves the limit, because a 429 is proof we
// were above the line rather than a guess about it.
func (l *adaptiveLimiter) throttle(retryAfter time.Duration) {
	l.mu.Lock()
	now := l.now()

	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	alreadyCooling := now.Before(l.coolUntil)
	if until := now.Add(retryAfter); until.After(l.coolUntil) {
		l.coolUntil = until
	}
	// One throttling EVENT halves the limit once. A CDN pushing back fails every
	// in-flight segment in the same instant, and each worker reports its own 429
	// — without this guard a single burst at limit 16 cascaded 16→8→4→2→min
	// within milliseconds, and climbing back from the floor took minutes. A
	// report arriving while the cool-down is still armed is an echo of the event
	// the first report already answered (it still extends the pause above); a
	// 429 after the cool-down expired is a new event and halves again.
	if alreadyCooling {
		l.mu.Unlock()
		return
	}

	// Any ceiling we lifted was justified by throughput alone; a 429 is the CDN
	// answering that question directly, so the lift is given back before anything
	// else. The floor still wins — capLocked lets it override the policy cap.
	if l.ceiling > maxSegmentConcurrency {
		l.ceiling = maxSegmentConcurrency
	}

	if halved := l.limit / 2; halved > l.min {
		l.setLimitLocked(halved)
	} else {
		l.setLimitLocked(l.min)
	}

	// The in-flight window spans the throttling, so its rate says nothing about
	// the new limit. Drop it and start clean, and don't probe upward for a while.
	l.settledAt = now.Add(settleFor)
	l.lastRate = 0
	l.reprobeLimit, l.reprobeRate = 0, 0
	l.windowStart, l.windowBytes, l.windowDone = now, 0, 0

	limit, ceiling := l.limit, l.ceiling
	l.mu.Unlock()
	l.signal()

	l.logger.Debug("CDN throttled us, backing off",
		domain.F("retry_after", retryAfter.String()),
		domain.F("concurrency", limit),
		domain.F("ceiling", ceiling),
	)
}

// liftCeilingLocked buys one step of extra headroom, bounded by the hard ceiling.
// Caller holds mu.
func (l *adaptiveLimiter) liftCeilingLocked() {
	if l.ceiling >= l.hardCeiling {
		return
	}
	l.ceiling += ceilingStep
	if l.ceiling > l.hardCeiling {
		l.ceiling = l.hardCeiling
	}
	l.logger.Debug("link still had room at the cap, lifting it",
		domain.F("ceiling", l.ceiling),
		domain.F("hard_ceiling", l.hardCeiling),
	)
}

// advanceLocked applies one measurement window to the limit. Split out from done
// so the policy can be tested directly, without waiting on real clocks or sockets.
func (l *adaptiveLimiter) advanceLocked(rate float64, now time.Time) {
	prev := l.lastRate
	l.lastRate = rate

	// Settled: hold the limit until the re-probe timer expires.
	if now.Before(l.settledAt) {
		return
	}
	if l.reprobeLimit > 0 {
		base, rateBefore := l.reprobeLimit, l.reprobeRate
		l.reprobeLimit, l.reprobeRate = 0, 0
		if rate <= rateBefore*growThreshold {
			l.setLimitLocked(base)
			l.settledAt = now.Add(settleFor)
			return
		}
	}
	// A settled plateau must actually probe again, even if its rate stayed
	// constant. Previously it re-armed the settle timer forever at the same cap.
	if !l.settledAt.IsZero() && prev > 0 && rate >= prev*shrinkThreshold {
		l.settledAt = time.Time{}
		base := l.limit
		l.setLimitLocked(base + 1)
		if l.limit > base {
			l.reprobeLimit, l.reprobeRate = base, rate
		} else {
			l.settledAt = now.Add(settleFor)
		}
		return
	}
	l.settledAt = time.Time{}

	switch {
	case prev == 0:
		// First window of the episode (or the first after a throttle). There is
		// nothing to compare against yet, so take one step up and judge it next
		// window. No ceiling lift here — a speculative step is not evidence.
		l.setLimitLocked(l.limit + 1)
	case rate > prev*growThreshold:
		// More throughput than last window — the pipe still has room. If we are
		// pinned against the cap while that is happening, the cap is what we are
		// measuring rather than the link, so lift it and keep climbing. That is
		// the whole difference between a fibre link and a fixed 16.
		if l.limit >= l.capLocked() {
			l.liftCeilingLocked()
		}
		l.setLimitLocked(l.limit + 1)
	case rate < prev*shrinkThreshold:
		// Less throughput. Either we overshot or the link itself got worse; both
		// are answered the same way — step back and hold. If it was the link, the
		// re-probe after settleFor will find the room again.
		l.setLimitLocked(l.limit - 1)
		l.settledAt = now.Add(settleFor)
	default:
		// Within the dead band: this is the knee of the curve. Stay here.
		l.settledAt = now.Add(settleFor)
	}
}

// setLimitLocked clamps and applies a new limit. Caller holds mu.
func (l *adaptiveLimiter) setLimitLocked(n int) {
	if n < l.min {
		n = l.min
	}
	if cap := l.capLocked(); n > cap {
		n = cap
	}
	if n == l.limit {
		return
	}
	grew := n > l.limit
	l.limit = n
	if n > l.peak {
		l.peak = n
	}
	if grew {
		l.signal()
	}
}

// current reports the live limit (for logging and tests).
func (l *adaptiveLimiter) current() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

// peakLimit reports the highest limit the controller reached (for logging).
func (l *adaptiveLimiter) peakLimit() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peak
}

// signal nudges one waiter without blocking if nobody is listening.
func (l *adaptiveLimiter) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// parseRetryAfter reads an HTTP Retry-After header, which is either a delay in
// seconds or an absolute HTTP date. It returns 0 when the header is absent,
// unparseable, or already in the past.
func parseRetryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs <= 0 {
			return 0
		}
		// Saturate rather than overflow into a negative wait for a bad header.
		const maxSeconds = int64(1<<63-1) / int64(time.Second)
		return time.Duration(min(int64(secs), maxSeconds)) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}
