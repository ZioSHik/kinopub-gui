package hlsdownloader

import (
	"context"
	"sync"
	"time"
)

const playbackLeaseTTL = 45 * time.Second

// SetPlaybackLease keeps background downloads parked across the player's
// buffering gaps. Leases expire if a tab crashes without sending its close.
func (l *adaptiveLimiter) SetPlaybackLease(id string, active bool) {
	l.mu.Lock()
	if l.playbackLeases == nil {
		l.playbackLeases = make(map[string]time.Time)
	}
	if active {
		l.playbackLeases[id] = l.now().Add(playbackLeaseTTL)
	} else {
		delete(l.playbackLeases, id)
	}
	l.mu.Unlock()
	l.signal()
}

func (l *adaptiveLimiter) playbackActiveLocked() bool {
	for id, until := range l.playbackLeases {
		if !l.now().Before(until) {
			delete(l.playbackLeases, id)
		}
	}
	return l.playbackRequests > 0 || len(l.playbackLeases) > 0
}

func (l *adaptiveLimiter) waitReady(ctx context.Context, playback bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.mu.Lock()
		wait := max(l.coolUntil.Sub(l.now()), l.sessionUntil.Sub(l.now()))
		ready := wait <= 0 && (playback || !l.playbackActiveLocked())
		l.mu.Unlock()
		if ready {
			return nil
		}
		if err := l.waitChange(ctx, max(wait, acquirePoll)); err != nil {
			return err
		}
	}
}

func (l *adaptiveLimiter) waitChange(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.wake:
		return nil
	case <-timer.C:
		return nil
	}
}

func (l *adaptiveLimiter) beginEpisode(ctx context.Context) (func(), error) {
	if !l.serializeDownloads {
		return func() {}, nil
	}
	for {
		if err := l.waitReady(ctx, false); err != nil {
			return nil, err
		}
		l.mu.Lock()
		ready := !l.episodeActive && !l.playbackActiveLocked() && !l.now().Before(l.sessionUntil) && !l.now().Before(l.coolUntil)
		if ready {
			l.episodeActive = true
		}
		l.mu.Unlock()
		if ready {
			var once sync.Once
			return func() {
				once.Do(func() {
					l.mu.Lock()
					l.episodeActive = false
					l.mu.Unlock()
					l.signal()
				})
			}, nil
		}
		if err := l.waitChange(ctx, acquirePoll); err != nil {
			return nil, err
		}
	}
}

// AcquirePlayback uses the same request budget and cooldown as downloads, with
// priority over new background requests. The slot lasts until the body closes.
func (l *adaptiveLimiter) AcquirePlayback(ctx context.Context) (func(), error) {
	l.mu.Lock()
	l.playbackRequests++
	l.mu.Unlock()
	l.signal()
	releasePriority := func() {
		l.mu.Lock()
		l.playbackRequests--
		l.mu.Unlock()
		l.signal()
	}
	if err := l.acquireFor(ctx, true); err != nil {
		releasePriority()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			l.release()
			releasePriority()
		})
	}, nil
}
