package gui

import (
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// eventReporter implements domain.ProgressReporter plus every optional progress
// sink the engine probes for (ByteProgressSink, SegmentProgressSink,
// HLSProgressSink) and the optional EpisodeDeferred hook. It folds each update
// into its Job and asks the manager to broadcast.
type eventReporter struct {
	mgr *JobManager
	job *Job
}

func newEventReporter(mgr *JobManager, job *Job) *eventReporter {
	return &eventReporter{mgr: mgr, job: job}
}

// Compile-time checks that we satisfy every interface the engine may assert.
var (
	_ domain.ProgressReporter   = (*eventReporter)(nil)
	_ domain.ByteProgressSink   = (*eventReporter)(nil)
	_ domain.SegmentProgressSink = (*eventReporter)(nil)
	_ domain.HLSProgressSink    = (*eventReporter)(nil)
)

func (r *eventReporter) Start(plan domain.SeriesPlan) {
	r.job.mu.Lock()
	if plan.Title != "" && r.job.title == "" {
		r.job.title = plan.Title
	}
	seasons := make(map[int]int, len(plan.Seasons))
	for k, v := range plan.Seasons {
		seasons[k] = v
	}
	r.job.plan = &PlanView{
		Title:            plan.Title,
		Total:            plan.Total,
		AlreadyCompleted: plan.AlreadyCompleted,
		Seasons:          seasons,
	}
	r.job.mu.Unlock()
	r.mgr.publishNow(r.job)
}

func (r *eventReporter) EpisodeStarted(key domain.EpisodeKey) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	ev.State = epRunning
	if ev.Percent >= 100 {
		ev.Percent = 0
	}
	ev.Error = ""
	ev.lastTime = time.Time{}
	ev.lastBytes = 0
	r.job.mu.Unlock()
	r.mgr.publishNow(r.job)
}

func (r *eventReporter) TrackProgress(key domain.EpisodeKey, track domain.TrackRef, percent int) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	if ev.State == epPending {
		ev.State = epRunning
	}
	// For non-HLS (single-stream) downloads the overall percent tracks the
	// reported track percent. HLS overrides Percent via SegmentProgress.
	if len(ev.Tracks) == 0 && percent > ev.Percent {
		ev.Percent = clampPct(percent)
	}
	r.job.mu.Unlock()
	r.mgr.publish(r.job)
}

func (r *eventReporter) EpisodeCompleted(key domain.EpisodeKey) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	ev.State = epCompleted
	ev.Percent = 100
	ev.SpeedBps = 0
	ev.ETASeconds = 0
	ev.Error = ""
	r.job.mu.Unlock()
	r.mgr.publishNow(r.job)
}

func (r *eventReporter) EpisodeFailed(key domain.EpisodeKey, err error) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	// Don't clobber a deferred state with a generic failure; the deferred hook
	// fires separately. Mark failed only if not already parked for retry.
	if ev.State != epDeferred {
		ev.State = epFailed
	}
	ev.SpeedBps = 0
	ev.ETASeconds = 0
	if err != nil {
		ev.Error = err.Error()
	}
	r.job.mu.Unlock()
	r.mgr.publishNow(r.job)
}

// EpisodeDeferred is the optional hook the engine calls when an episode is
// parked for a later retry after a transient failure.
func (r *eventReporter) EpisodeDeferred(key domain.EpisodeKey, err error, attempts int) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	ev.State = epDeferred
	ev.Attempts = attempts
	ev.SpeedBps = 0
	ev.ETASeconds = 0
	if err != nil {
		ev.Error = err.Error()
	}
	r.job.mu.Unlock()
	r.mgr.publishNow(r.job)
}

func (r *eventReporter) Stop() {}

// ByteProgress reports bytes downloaded out of total for progressive downloads.
func (r *eventReporter) ByteProgress(key domain.EpisodeKey, downloaded, total int64) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	ev.Bytes = downloaded
	ev.Total = total
	if total > 0 {
		ev.Percent = clampPct(int(downloaded * 100 / total))
	}
	r.updateSpeed(ev, downloaded, total)
	r.job.mu.Unlock()
	r.mgr.publish(r.job)
}

// SegmentProgress reports HLS segment-level progress with an approximate total.
func (r *eventReporter) SegmentProgress(key domain.EpisodeKey, doneSegments, totalSegments int, downloadedBytes, approxTotalBytes int64) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	ev.SegDone = doneSegments
	ev.SegTotal = totalSegments
	ev.Bytes = downloadedBytes
	ev.Total = approxTotalBytes
	if totalSegments > 0 {
		ev.Percent = clampPct(doneSegments * 100 / totalSegments)
	}
	r.updateSpeed(ev, downloadedBytes, approxTotalBytes)
	r.job.mu.Unlock()
	r.mgr.publish(r.job)
}

// HLSProgress reports the full per-track breakdown for nested progress bars.
func (r *eventReporter) HLSProgress(key domain.EpisodeKey, tracks []domain.TrackProgressInfo) {
	r.job.mu.Lock()
	ev := r.job.ensureEpisode(key)
	views := make([]TrackView, 0, len(tracks))
	for _, t := range tracks {
		pct := 0
		if t.TotalSegments > 0 {
			pct = clampPct(t.DoneSegments * 100 / t.TotalSegments)
		}
		views = append(views, TrackView{
			Label:       t.Label,
			Percent:     pct,
			Done:        t.DoneSegments,
			Total:       t.TotalSegments,
			Bytes:       t.DownloadedBytes,
			ApproxTotal: t.ApproxTotalBytes,
		})
	}
	ev.Tracks = views
	r.job.mu.Unlock()
	r.mgr.publish(r.job)
}

// updateSpeed maintains a smoothed download speed and ETA. Caller holds j.mu.
func (r *eventReporter) updateSpeed(ev *EpisodeView, downloaded, total int64) {
	now := time.Now()
	if !ev.lastTime.IsZero() {
		dt := now.Sub(ev.lastTime).Seconds()
		if dt >= 0.25 {
			inst := float64(downloaded-ev.lastBytes) / dt
			if inst < 0 {
				inst = 0
			}
			if ev.SpeedBps == 0 {
				ev.SpeedBps = inst
			} else {
				// Exponential moving average to smooth jitter.
				ev.SpeedBps = ev.SpeedBps*0.7 + inst*0.3
			}
			if ev.SpeedBps > 0 && total > downloaded {
				ev.ETASeconds = int(float64(total-downloaded) / ev.SpeedBps)
			}
			ev.lastBytes = downloaded
			ev.lastTime = now
		}
	} else {
		ev.lastBytes = downloaded
		ev.lastTime = now
	}
}

func clampPct(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
