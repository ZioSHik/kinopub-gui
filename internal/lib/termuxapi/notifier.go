// Package termuxapi integrates with the Termux:API app to show download
// progress in Android notifications.
//
// All functions are no-ops when termux-notification is not in PATH, so
// the package is safe to use unconditionally on any platform.
package termuxapi

import (
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// termux-notification --id requires an integer.
const notificationID = "42314"

// Available reports whether Termux:API tools are present in PATH.
func Available() bool {
	_, err := exec.LookPath("termux-notification")
	return err == nil
}

// Notifier wraps a domain.ProgressReporter and mirrors progress into
// Android notifications via termux-notification.
//
// It also implements HLSProgressSink, SegmentProgressSink and ByteProgressSink
// by forwarding to inner, so the live terminal display keeps its full
// per-track breakdown even when wrapped.
type Notifier struct {
	inner domain.ProgressReporter

	mu          sync.Mutex
	seriesTitle string
	completed   int
	total       int

	// current episode state (updated concurrently by TrackProgress)
	currentEp  atomic.Value // string
	currentPct atomic.Int32

	// throttle: skip notification if previous goroutine is still running
	notifying atomic.Bool
}

// Wrap returns a new Notifier that delegates to inner and adds Termux
// notifications. If Termux:API is not available, inner is returned as-is.
func Wrap(inner domain.ProgressReporter) domain.ProgressReporter {
	if !Available() {
		return inner
	}
	return &Notifier{inner: inner}
}

// ---------------------------------------------------------------------------
// domain.ProgressReporter
// ---------------------------------------------------------------------------

func (n *Notifier) Start(plan domain.SeriesPlan) {
	n.mu.Lock()
	n.total = plan.Total
	n.seriesTitle = plan.Title
	n.mu.Unlock()

	n.inner.Start(plan)
	n.notify(fmt.Sprintf("kinopub — %s", plan.Title),
		fmt.Sprintf("Начало загрузки · %d эп.", plan.Total), 0)
}

func (n *Notifier) EpisodeStarted(key domain.EpisodeKey) {
	label := fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)
	n.currentEp.Store(label)
	n.currentPct.Store(0)
	n.inner.EpisodeStarted(key)
	n.refresh()
}

func (n *Notifier) TrackProgress(key domain.EpisodeKey, track domain.TrackRef, percent int) {
	n.currentPct.Store(int32(percent))
	n.inner.TrackProgress(key, track, percent)
	n.refresh()
}

func (n *Notifier) EpisodeCompleted(key domain.EpisodeKey) {
	n.mu.Lock()
	n.completed++
	done := n.completed
	total := n.total
	title := n.seriesTitle
	n.mu.Unlock()

	n.inner.EpisodeCompleted(key)

	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	ep := fmt.Sprintf("S%02dE%02d", key.Season, key.Episode)
	n.notify(
		fmt.Sprintf("kinopub %d%% — %s", pct, title),
		fmt.Sprintf("%s готово · %d/%d эп.", ep, done, total),
		pct,
	)
}

func (n *Notifier) EpisodeFailed(key domain.EpisodeKey, err error) {
	n.inner.EpisodeFailed(key, err)
}

func (n *Notifier) Stop() {
	n.inner.Stop()

	n.mu.Lock()
	done := n.completed
	total := n.total
	title := n.seriesTitle
	n.mu.Unlock()

	if done > 0 && done >= total {
		exec.Command("termux-notification", //nolint:errcheck
			"--id", notificationID,
			"--title", fmt.Sprintf("✓ %s", title),
			"--content", fmt.Sprintf("Скачано %d эпизодов", done),
		).Run()
		exec.Command("termux-vibrate", "-d", "400").Run() //nolint:errcheck
	} else {
		exec.Command("termux-notification-remove", notificationID).Run() //nolint:errcheck
	}
}

// ---------------------------------------------------------------------------
// Optional sink interfaces — forwarded to inner so the live terminal display
// keeps the full per-track HLS breakdown.
// ---------------------------------------------------------------------------

func (n *Notifier) HLSProgress(key domain.EpisodeKey, tracks []domain.TrackProgressInfo) {
	if s, ok := n.inner.(domain.HLSProgressSink); ok {
		s.HLSProgress(key, tracks)
	}
}

func (n *Notifier) SegmentProgress(key domain.EpisodeKey, done, total int, downloaded, approxTotal int64) {
	if s, ok := n.inner.(domain.SegmentProgressSink); ok {
		s.SegmentProgress(key, done, total, downloaded, approxTotal)
	}
}

func (n *Notifier) ByteProgress(key domain.EpisodeKey, downloaded, total int64) {
	if s, ok := n.inner.(domain.ByteProgressSink); ok {
		s.ByteProgress(key, downloaded, total)
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (n *Notifier) refresh() {
	// Skip if a notification command is already in flight — TrackProgress fires
	// very frequently and termux-notification is slow (~200ms).
	if !n.notifying.CompareAndSwap(false, true) {
		return
	}

	ep, _ := n.currentEp.Load().(string)
	pct := int(n.currentPct.Load())

	n.mu.Lock()
	done := n.completed
	total := n.total
	title := n.seriesTitle
	n.mu.Unlock()

	seriesPct := 0
	if total > 0 {
		seriesPct = done*100/total + pct/total
	}
	titleStr := fmt.Sprintf("kinopub ↓ %d%% — %s", seriesPct, title)
	content := fmt.Sprintf("%s · %d/%d эп.  %d%%", ep, done, total, pct)

	go func() {
		defer n.notifying.Store(false)
		exec.Command("termux-notification", //nolint:errcheck
			"--id", notificationID,
			"--title", titleStr,
			"--content", content,
			"--ongoing",
			"--priority", "low",
			"--progress-max", "100",
			"--progress", strconv.Itoa(seriesPct),
		).Run()
	}()
}

func (n *Notifier) notify(title, content string, pct int) {
	exec.Command("termux-notification", //nolint:errcheck
		"--id", notificationID,
		"--title", title,
		"--content", content,
		"--ongoing",
		"--priority", "low",
		"--progress-max", "100",
		"--progress", strconv.Itoa(pct),
	).Run()
}
