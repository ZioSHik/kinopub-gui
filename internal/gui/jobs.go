package gui

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/app/kinopub"
	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

// Job status values.
const (
	statusQueued    = "queued"
	statusResolving = "resolving"
	statusRunning   = "running"
	statusCompleted = "completed"
	statusFailed    = "failed"
	statusCanceled  = "canceled"
	statusPaused    = "paused"
)

// Episode lifecycle states surfaced to the UI.
const (
	epPending   = "pending"
	epRunning   = "running"
	epCompleted = "completed"
	epFailed    = "failed"
	epDeferred  = "deferred"
	epPaused    = "paused"
)

const maxJobLogs = 400

// LogEntry is a single engine log line streamed to the UI.
type LogEntry struct {
	Time      time.Time      `json:"time"`
	Level     string         `json:"level"`
	Component string         `json:"component,omitempty"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// TrackView is a per-track progress row (HLS video + audio renditions).
type TrackView struct {
	Label       string `json:"label"`
	Percent     int    `json:"percent"`
	Done        int    `json:"done"`
	Total       int    `json:"total"`
	Bytes       int64  `json:"bytes"`
	ApproxTotal int64  `json:"approxTotal"`
}

// EpisodeView is the per-episode progress as shown in the UI.
type EpisodeView struct {
	Key     string `json:"key"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Percent int    `json:"percent"`
	Bytes   int64  `json:"bytes"`
	Total   int64  `json:"total"`
	// TotalApprox marks Total as an estimate rather than a known size. HLS has no
	// declared total, so it's extrapolated from the average segment size and drifts
	// as it downloads; progressive downloads report the real Content-Length.
	TotalApprox bool        `json:"totalApprox"`
	SpeedBps    float64     `json:"speedBps"`
	ETASeconds  int         `json:"etaSeconds"`
	SegDone     int         `json:"segDone"`
	SegTotal    int         `json:"segTotal"`
	Tracks      []TrackView `json:"tracks,omitempty"`
	Attempts    int         `json:"attempts"`
	Error       string      `json:"error,omitempty"`

	// internal speed-sampling state (not serialized)
	lastBytes int64
	lastTime  time.Time
}

// PlanView mirrors domain.SeriesPlan for the UI.
type PlanView struct {
	Title            string      `json:"title"`
	Total            int         `json:"total"`
	AlreadyCompleted int         `json:"alreadyCompleted"`
	Seasons          map[int]int `json:"seasons,omitempty"`
}

// SummaryView mirrors domain.RunResult for the UI.
type SummaryView struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// AudioRequestView describes a pending interactive audio-track choice.
type AudioRequestView struct {
	Tracks         []domain.AudioTrackInfo `json:"tracks"`
	TimeoutSeconds int                     `json:"timeoutSeconds"`
	DeadlineUnix   int64                   `json:"deadlineUnix"`
}

// JobView is the full serializable snapshot of a job sent to the UI.
type JobView struct {
	ID           string            `json:"id"`
	URL          string            `json:"url"`
	Status       string            `json:"status"`
	Title        string            `json:"title"`
	PosterURL    string            `json:"posterUrl,omitempty"`
	OutputPath   string            `json:"outputPath"`
	DryRun       bool              `json:"dryRun"`
	Quality      string            `json:"quality"`
	CreatedAt    time.Time         `json:"createdAt"`
	StartedAt    *time.Time        `json:"startedAt,omitempty"`
	FinishedAt   *time.Time        `json:"finishedAt,omitempty"`
	Plan         *PlanView         `json:"plan,omitempty"`
	Episodes     []EpisodeView     `json:"episodes"`
	Summary      *SummaryView      `json:"summary,omitempty"`
	Error        string            `json:"error,omitempty"`
	PendingAudio *AudioRequestView `json:"pendingAudio,omitempty"`
	Logs         []LogEntry        `json:"logs"`
}

// Job is the live, mutable server-side representation of a download run.
type Job struct {
	mu sync.Mutex

	id         string
	url        string
	status     string
	title      string
	posterURL  string
	outputPath string
	dryRun     bool
	quality    string
	createdAt  time.Time
	startedAt  *time.Time
	finishedAt *time.Time

	plan     *PlanView
	episodes map[string]*EpisodeView // key "S%dE%d"
	titles   map[string]string       // seeded from preview, key "S%dE%d"
	summary  *SummaryView
	errMsg   string

	// cfg and seedTitles are retained so a finished job can be re-run verbatim
	// ("retry"); the engine skips already-completed episodes via the state store,
	// so a retry re-downloads only what failed.
	cfg        domain.RunConfig
	seedTitles map[string]string

	pendingAudio *AudioRequestView
	audioAnswer  chan []int // delivers the interactive audio selection
	logs         []LogEntry

	// prioritize carries "download next" requests from the UI to the running
	// engine, which drains it between episodes. Buffered so a burst of clicks
	// never blocks the HTTP handler; sends are best-effort (dropped when full).
	prioritize chan domain.EpisodeKey

	// pauseEp / resumeEp hold or release an individual episode (including one that
	// is actively downloading); retryEp re-queues a failed episode in place;
	// cancelEp drops an episode from the run entirely (its siblings keep going).
	// The running engine's control goroutine drains them. Buffered; best-effort
	// sends.
	pauseEp  chan domain.EpisodeKey
	resumeEp chan domain.EpisodeKey
	retryEp  chan domain.EpisodeKey
	cancelEp chan domain.EpisodeKey

	// paused reports whether the active run is being paused (vs. canceled), so the
	// engine preserves partial segment data for a later resume. Read by the engine
	// via deps.Paused; reset at the start of every run.
	paused atomic.Bool

	// retryOnly scopes the NEXT run to specific episodes (a per-episode retry of a
	// finished job), so it re-downloads only those — not every not-yet-completed
	// episode. Consumed (cleared) at the start of each run. Guarded by mu.
	retryOnly []domain.EpisodeKey

	cancel          context.CancelFunc
	cancelRequested bool            // set if canceled before its engine started
	urgent          bool            // scheduler: may bypass maxActive (guarded by JobManager.mu)
	done            <-chan struct{} // closed when the job's context is canceled/finished
	dirty           bool            // pending broadcast
}

func newJob(id, url string, cfg domain.RunConfig) *Job {
	return &Job{
		id:         id,
		url:        url,
		status:     statusQueued,
		outputPath: cfg.OutputPath,
		dryRun:     cfg.DryRun,
		quality:    string(cfg.Quality),
		createdAt:  time.Now(),
		episodes:   make(map[string]*EpisodeView),
		titles:     make(map[string]string),
		cfg:        cfg,
		prioritize: make(chan domain.EpisodeKey, 128),
		pauseEp:    make(chan domain.EpisodeKey, 128),
		resumeEp:   make(chan domain.EpisodeKey, 128),
		retryEp:    make(chan domain.EpisodeKey, 128),
		cancelEp:   make(chan domain.EpisodeKey, 128),
	}
}

// epKey formats an episode key as "S{n}E{n}" (matching the state store keys).
func epKey(k domain.EpisodeKey) string {
	return fmt.Sprintf("S%dE%d", k.Season, k.Episode)
}

// ensureEpisode returns the EpisodeView for a key, creating it if needed.
// Caller must hold j.mu.
func (j *Job) ensureEpisode(k domain.EpisodeKey) *EpisodeView {
	key := epKey(k)
	ev, ok := j.episodes[key]
	if !ok {
		ev = &EpisodeView{
			Key:     key,
			Season:  k.Season,
			Episode: k.Episode,
			Title:   j.titles[key],
			State:   epPending,
		}
		j.episodes[key] = ev
	}
	return ev
}

// snapshot builds an immutable JobView. Caller must NOT hold j.mu.
func (j *Job) snapshot() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snapshotLocked()
}

func (j *Job) snapshotLocked() JobView {
	eps := make([]EpisodeView, 0, len(j.episodes))
	for _, ev := range j.episodes {
		eps = append(eps, *ev)
	}
	sort.Slice(eps, func(a, b int) bool {
		if eps[a].Season != eps[b].Season {
			return eps[a].Season < eps[b].Season
		}
		return eps[a].Episode < eps[b].Episode
	})
	logs := make([]LogEntry, len(j.logs))
	copy(logs, j.logs)

	view := JobView{
		ID:           j.id,
		URL:          j.url,
		Status:       j.status,
		Title:        j.title,
		PosterURL:    j.posterURL,
		OutputPath:   j.outputPath,
		DryRun:       j.dryRun,
		Quality:      j.quality,
		CreatedAt:    j.createdAt,
		StartedAt:    j.startedAt,
		FinishedAt:   j.finishedAt,
		Plan:         j.plan,
		Episodes:     eps,
		Summary:      j.summary,
		Error:        j.errMsg,
		PendingAudio: j.pendingAudio,
		Logs:         logs,
	}
	return view
}

func (j *Job) addLog(e LogEntry) {
	j.mu.Lock()
	j.logs = append(j.logs, e)
	if len(j.logs) > maxJobLogs {
		j.logs = j.logs[len(j.logs)-maxJobLogs:]
	}
	j.dirty = true
	j.mu.Unlock()
}

func (j *Job) finished() bool {
	switch j.status {
	case statusCompleted, statusFailed, statusCanceled:
		return true
	}
	return false
}

// JobManager owns all jobs and the broadcast hub.
type JobManager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	seq  int

	hub *Hub

	// store persists the queue to disk so unfinished downloads survive a restart
	// (restored as paused/failed cards that can be resumed). persistGen is bumped
	// on every job change; persistLoop writes a snapshot when it advances, and
	// remove/clear persist synchronously so deletions aren't resurrected by a
	// quick restart. store is nil in tests that don't attach one.
	store        *jobStore
	persistGen   atomic.Int64
	persistedGen int64

	// Global download scheduler. maxActive bounds how many jobs run at once
	// (0 = unlimited); extra jobs wait in pending (FIFO, reorderable) and are
	// dispatched as running slots free up. startFn launches a job's run goroutine
	// and is injected by the server (it needs the API client). These are guarded
	// by mu.
	maxActive int
	running   int
	pending   []*Job
	startFn   func(*Job)
}

func newJobManager(hub *Hub) *JobManager {
	m := &JobManager{
		jobs: make(map[string]*Job),
		hub:  hub,
	}
	go m.flushLoop()
	return m
}

// attachStore wires queue persistence: it restores the persisted jobs into the
// manager (in-flight ones come back paused, see restoreJob) and starts the
// background persist loop. Must be called before the server starts serving.
func (m *JobManager) attachStore(store *jobStore) {
	m.mu.Lock()
	m.store = store
	for _, p := range store.load() {
		if _, exists := m.jobs[p.ID]; exists || p.ID == "" {
			continue
		}
		j := restoreJob(p)
		m.jobs[j.id] = j
		// Keep the id sequence ahead of every restored id so new jobs never
		// collide ("job-7" restored → next new job is at least "job-8").
		var n int
		if _, err := fmt.Sscanf(j.id, "job-%d", &n); err == nil && n > m.seq {
			m.seq = n
		}
	}
	m.mu.Unlock()
	go m.persistLoop()
}

// persistLoop writes the queue snapshot whenever something changed since the
// last write. The cadence bounds both disk churn and how much progress display
// a crash can lose (the engine's own state/segments are persisted separately —
// only the card's cosmetic counters are at stake).
func (m *JobManager) persistLoop() {
	defer func() {
		if r := recover(); r != nil {
			go m.persistLoop()
		}
	}()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for range t.C {
		m.persistNow()
	}
}

// persistNow writes the current queue to disk if it changed since the last
// write. Safe for concurrent use; no-op without an attached store.
func (m *JobManager) persistNow() {
	m.mu.Lock()
	store := m.store
	gen := m.persistGen.Load()
	if store == nil || gen == m.persistedGen {
		m.mu.Unlock()
		return
	}
	m.persistedGen = gen
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()

	out := make([]persistedJob, 0, len(jobs))
	for _, j := range jobs {
		p := persistedFrom(j)
		// Dry-run cards are previews — nothing on disk to resume; don't restore.
		if p.Cfg.DryRun {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.Before(out[b].CreatedAt) })
	_ = store.save(out) // best-effort: a failed write retries on the next change
}

// markPersistDirty schedules a queue write on the next persist tick.
func (m *JobManager) markPersistDirty() { m.persistGen.Add(1) }

// setMaxActive updates the global concurrency limit and dispatches any jobs the
// new headroom now allows (e.g. the user raised the limit). n <= 0 means no
// limit.
func (m *JobManager) setMaxActive(n int) {
	m.mu.Lock()
	m.maxActive = n
	m.mu.Unlock()
	m.dispatch()
}

// submit registers a job and queues it for dispatch. front=true inserts it at
// the head of the wait queue (used for per-episode retries, which should not
// wait behind unrelated downloads). The job starts immediately if a slot is
// free (or the limit is unlimited).
func (m *JobManager) submit(j *Job, front bool) {
	m.add(j)
	m.mu.Lock()
	if front {
		// Front-of-queue jobs are per-episode retries: besides jumping the line
		// they bypass the global concurrency limit so they truly start "now",
		// even when the parent download already occupies every slot.
		j.urgent = true
		m.pending = append([]*Job{j}, m.pending...)
	} else {
		m.pending = append(m.pending, j)
	}
	m.mu.Unlock()
	m.publishNow(j)
	m.dispatch()
}

// dispatch starts as many pending jobs as the concurrency limit allows. startFn
// is invoked outside the lock so a job's goroutine launch can't deadlock the
// scheduler.
func (m *JobManager) dispatch() {
	m.mu.Lock()
	var toStart []*Job
	for len(m.pending) > 0 {
		j := m.pending[0]
		hasSlot := m.maxActive <= 0 || m.running < m.maxActive
		// Start when within the limit, or when the head job is urgent (a
		// per-episode retry) — urgent jobs accept transient over-subscription,
		// which self-balances as running jobs finish. A non-urgent head with no
		// slot blocks the queue (FIFO), so it waits for a slot.
		if !hasSlot && !j.urgent {
			break
		}
		m.pending = m.pending[1:]
		m.running++
		toStart = append(toStart, j)
	}
	fn := m.startFn
	m.mu.Unlock()
	if fn == nil {
		return
	}
	for _, j := range toStart {
		fn(j)
	}
}

// jobFinished is called once when a running job's goroutine exits; it frees the
// job's slot and dispatches the next waiting job.
func (m *JobManager) jobFinished() {
	m.mu.Lock()
	if m.running > 0 {
		m.running--
	}
	m.mu.Unlock()
	m.dispatch()
}

// prioritizeJob moves a still-queued job to the head of the wait queue so it is
// dispatched before the other waiting jobs. Returns false if the job is unknown
// or not currently waiting (already running/finished — nothing to reorder).
func (m *JobManager) prioritizeJob(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.pending {
		if j.id == id {
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			m.pending = append([]*Job{j}, m.pending...)
			return true
		}
	}
	return false
}

// dropPending removes a job from the wait queue if present (e.g. it was canceled
// before it ever started). Returns true if it was waiting.
func (m *JobManager) dropPending(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.pending {
		if j.id == id {
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			return true
		}
	}
	return false
}

// flushLoop periodically broadcasts jobs marked dirty, bounding the event rate
// so high-frequency progress updates don't overwhelm the SSE stream. It recovers
// from any panic and relaunches itself, so a single bad broadcast can never
// silently stop live progress for the whole server.
func (m *JobManager) flushLoop() {
	defer func() {
		if r := recover(); r != nil {
			go m.flushLoop()
		}
	}()
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		m.mu.RLock()
		jobs := make([]*Job, 0, len(m.jobs))
		for _, j := range m.jobs {
			jobs = append(jobs, j)
		}
		m.mu.RUnlock()
		for _, j := range jobs {
			j.mu.Lock()
			dirty := j.dirty
			j.dirty = false
			var view JobView
			if dirty {
				view = j.snapshotLocked()
			}
			j.mu.Unlock()
			if dirty {
				m.hub.broadcast(Event{Type: "job", Data: view})
			}
		}
	}
}

// publish marks a job dirty for the next throttled flush.
func (m *JobManager) publish(j *Job) {
	j.mu.Lock()
	j.dirty = true
	j.mu.Unlock()
	m.markPersistDirty()
}

// publishNow broadcasts a job immediately (used for important transitions).
func (m *JobManager) publishNow(j *Job) {
	j.mu.Lock()
	j.dirty = false
	view := j.snapshotLocked()
	j.mu.Unlock()
	m.hub.broadcast(Event{Type: "job", Data: view})
	m.markPersistDirty()
}

func (m *JobManager) nextID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return fmt.Sprintf("job-%d", m.seq)
}

func (m *JobManager) add(j *Job) {
	m.mu.Lock()
	m.jobs[j.id] = j
	m.mu.Unlock()
}

func (m *JobManager) get(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	return j, ok
}

// outputPaths returns the distinct output directories of all known jobs, so the
// /api/open handler can permit revealing files a job downloaded even when the
// job used a one-off output folder not saved in settings.
func (m *JobManager) outputPaths() []string {
	m.mu.RLock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.RUnlock()
	seen := make(map[string]bool)
	var paths []string
	for _, j := range jobs {
		j.mu.Lock()
		p := j.outputPath
		j.mu.Unlock()
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths
}

func (m *JobManager) list() []JobView {
	m.mu.RLock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.RUnlock()
	views := make([]JobView, 0, len(jobs))
	for _, j := range jobs {
		views = append(views, j.snapshot())
	}
	sort.Slice(views, func(a, b int) bool {
		return views[a].CreatedAt.After(views[b].CreatedAt)
	})
	return views
}

// remove deletes a finished job; returns false if it is still running.
func (m *JobManager) remove(id string) (bool, bool) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return false, false
	}
	if !j.finished() && j.status != statusPaused {
		m.mu.Unlock()
		return false, true // exists but active (running/queued) — must be stopped first
	}
	delete(m.jobs, id)
	m.mu.Unlock()
	m.hub.broadcast(Event{Type: "job_removed", Data: map[string]string{"id": id}})
	// Persist synchronously so a removed card can't be resurrected by a restart
	// that happens before the next persist tick.
	m.markPersistDirty()
	m.persistNow()
	return true, true
}

// clearFinished removes all finished jobs and returns how many were removed.
func (m *JobManager) clearFinished() int {
	m.mu.Lock()
	removed := make([]string, 0)
	for id, j := range m.jobs {
		if j.finished() {
			delete(m.jobs, id)
			removed = append(removed, id)
		}
	}
	m.mu.Unlock()
	for _, id := range removed {
		m.hub.broadcast(Event{Type: "job_removed", Data: map[string]string{"id": id}})
	}
	if len(removed) > 0 {
		m.markPersistDirty()
		m.persistNow()
	}
	return len(removed)
}

// run executes a download job end-to-end: it seeds episode metadata (best
// effort), wires the GUI reporter/chooser/logger, runs the engine, and records
// the outcome. It is meant to run in its own goroutine.
func (m *JobManager) run(parent context.Context, j *Job, cfg domain.RunConfig, titles map[string]string, title, poster string, apiClient *kinopubapi.Client) {
	// Free this job's scheduler slot and dispatch the next queued job when the
	// run exits, on every path (success, failure, panic, early return). Registered
	// first so it runs last — after the recover below has finalized the status.
	defer m.jobFinished()
	ctx, cancel := context.WithCancel(parent)
	// A panic anywhere in the run path must fail just this job, not crash the
	// whole server (and every other in-flight download).
	defer func() {
		if r := recover(); r != nil {
			m.failJob(j, fmt.Sprintf("internal error: %v", r))
		}
	}()
	defer cancel()
	j.mu.Lock()
	j.cancel = cancel
	j.done = ctx.Done()
	j.status = statusResolving
	now := time.Now()
	j.startedAt = &now
	if title != "" {
		j.title = title
	}
	if poster != "" {
		j.posterURL = poster
	}
	for k, v := range titles {
		j.titles[k] = v
	}
	canceledEarly := j.cancelRequested
	// Consume a per-episode retry scope for THIS run so only the requested
	// episodes are re-downloaded (the plan still covers the full series).
	if len(j.retryOnly) > 0 {
		cfg.RetryOnly = j.retryOnly
		j.retryOnly = nil
	}
	j.mu.Unlock()
	// Honor a pause/cancel that arrived after dispatch but before the cancel func
	// was installed (so a Pause/Stop click on a just-started job is never lost).
	// paused is checked first: run() finalization turns a paused stop into the
	// "paused" status (preserving progress) rather than "canceled".
	if j.paused.Load() || canceledEarly {
		cancel()
	}
	m.publishNow(j)

	reporter := newEventReporter(m, j)
	logger := newUILogger(m, j, cfg.Verbosity)

	var chooser domain.AudioChooser
	if cfg.AudioMenu {
		chooser = newGUIChooser(m, j)
	}

	if apiClient == nil {
		m.failJob(j, "not signed in to kino.pub — sign in in Settings to download")
		return
	}
	deps, err := buildEngineDeps(cfg, apiClient, logger, reporter, chooser, j.prioritize, j.pauseEp, j.resumeEp, j.retryEp, j.cancelEp, j.paused.Load)
	if err != nil {
		m.failJob(j, "setup failed: "+err.Error())
		return
	}

	app, err := kinopub.New(deps)
	if err != nil {
		m.failJob(j, "init failed: "+err.Error())
		return
	}

	j.mu.Lock()
	if j.status == statusResolving {
		j.status = statusRunning
	}
	j.mu.Unlock()
	m.publishNow(j)

	result, runErr := app.Run(ctx, cfg)

	j.mu.Lock()
	fin := time.Now()
	j.finishedAt = &fin
	j.pendingAudio = nil
	j.summary = &SummaryView{
		Total:     result.Total,
		Succeeded: result.Succeeded,
		Failed:    result.Failed,
		Skipped:   result.Skipped,
	}
	switch {
	case j.paused.Load():
		// Paused (not canceled): keep progress; episodes are held as "paused" and
		// the job can be resumed, which re-runs and continues from .hls-tmp.
		j.status = statusPaused
		j.errMsg = ""
	case ctx.Err() != nil:
		j.status = statusCanceled
		if j.errMsg == "" {
			j.errMsg = "canceled"
		}
	case runErr != nil:
		j.status = statusFailed
		j.errMsg = runErr.Error()
	case result.Failed > 0 && result.Succeeded == 0 && result.Total > 0:
		j.status = statusFailed
		j.errMsg = fmt.Sprintf("%d of %d episodes failed", result.Failed, result.Total)
	default:
		j.status = statusCompleted
	}
	if j.status == statusPaused {
		settlePausedEpisodesLocked(j)
	} else {
		settleUnfinishedEpisodesLocked(j, ctx.Err() != nil)
	}
	j.mu.Unlock()
	m.publishNow(j)
}

// settlePausedEpisodesLocked freezes every non-completed episode of a paused job
// in a "paused" view state (keeping its progress), so the card reads as paused
// rather than failed and a resume can continue. Caller must hold j.mu.
func settlePausedEpisodesLocked(j *Job) {
	for _, ev := range j.episodes {
		if ev.State == epPending || ev.State == epRunning || ev.State == epDeferred {
			ev.State = epPaused
			ev.SpeedBps = 0
			ev.ETASeconds = 0
		}
	}
}

// settleUnfinishedEpisodesLocked moves every non-completed episode of a finished
// job to "failed". A finished run must not leave episodes frozen in a transient
// view state: on cancel especially, the engine can re-park an episode as
// "deferred" (reporting "retrying…") in the same instant the workers exit, so
// the row would otherwise linger forever looking like a live retry on a job that
// has actually stopped — with no way to stop it. Settling to "failed" gives the
// UI a stable state with a working per-episode Retry. When the run was canceled,
// episodes that never recorded an error are stamped "canceled" for clarity.
// Caller must hold j.mu.
func settleUnfinishedEpisodesLocked(j *Job, canceled bool) {
	for _, ev := range j.episodes {
		if ev.State == epPending || ev.State == epRunning || ev.State == epDeferred {
			ev.State = epFailed
			ev.SpeedBps = 0
			ev.ETASeconds = 0
			if ev.Error == "" && canceled {
				ev.Error = "canceled"
			}
		}
	}
}

func (m *JobManager) failJob(j *Job, msg string) {
	j.mu.Lock()
	j.status = statusFailed
	j.errMsg = msg
	fin := time.Now()
	j.finishedAt = &fin
	j.mu.Unlock()
	m.publishNow(j)
}

// prioritizeEpisode asks a running job's engine to move an episode to the front
// of its download queue. Returns false if the job is unknown or not currently
// running (nothing to reorder). The send is non-blocking: if the buffer is full
// the request is dropped, which only means the queue keeps its current order.
func (m *JobManager) prioritizeEpisode(id string, key domain.EpisodeKey) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	live := j.status == statusRunning || j.status == statusResolving
	ch := j.prioritize
	j.mu.Unlock()
	if !live || ch == nil {
		return false
	}
	select {
	case ch <- key:
	default:
	}
	return true
}

// cancel stops a running job, or removes a still-queued one from the wait queue.
func (m *JobManager) cancelJob(id string) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	j.cancelRequested = true // honored by run() if its engine hasn't started yet
	cancel := j.cancel
	queued := j.status == statusQueued
	j.mu.Unlock()

	// Engine already running → cancel its context.
	if cancel != nil {
		cancel()
		return true
	}
	// No engine yet. If it is still waiting for a slot, drop it from the queue and
	// mark it canceled directly — it never incremented running, so jobFinished
	// must NOT fire for it. If dropPending fails it was just dispatched; run()
	// will honor cancelRequested as soon as it installs its cancel func.
	if queued && m.dropPending(id) {
		j.mu.Lock()
		j.status = statusCanceled
		if j.errMsg == "" {
			j.errMsg = "canceled"
		}
		fin := time.Now()
		j.finishedAt = &fin
		j.mu.Unlock()
		m.publishNow(j)
	}
	return true
}

// pauseJob pauses a job. A queued job is held out of dispatch; a running job is
// stopped with its partial progress preserved (so a later resume continues from
// where it left off). Returns false if the job is unknown or not in a pausable
// state. The paused flag is set BEFORE any dropPending/cancel so that, even if
// the job is dispatched concurrently, run() observes it and settles to the
// paused (not canceled) state.
func (m *JobManager) pauseJob(id string) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	status := j.status
	j.mu.Unlock()
	if status != statusQueued && status != statusResolving && status != statusRunning {
		return false
	}
	j.paused.Store(true)

	if status == statusQueued {
		if m.dropPending(id) {
			j.mu.Lock()
			j.status = statusPaused
			j.mu.Unlock()
			m.publishNow(j)
			return true
		}
		// Lost the race — it was just dispatched; fall through to stop its engine.
	}
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel() // run() finalization settles to statusPaused (paused flag is set)
	}
	// If cancel is nil the job was dispatched but its cancel func isn't installed
	// yet; run() checks the paused flag at startup and stops itself.
	return true
}

// resetEpisodesForRerunLocked flips every episode a fresh run will re-attempt
// (paused / failed / deferred — everything not completed) back to pending, so
// the card reflects the new run IMMEDIATELY. Without this the rows keep their
// stale paused/failed state (with live Resume/Retry buttons) for the whole
// resolve phase, which can hang for minutes on a flaky VPN — the engine's own
// reporter.Start reset only fires after resolve+plan succeed. Caller holds j.mu.
func resetEpisodesForRerunLocked(j *Job) {
	for _, ev := range j.episodes {
		if ev.State == epPaused || ev.State == epFailed || ev.State == epDeferred || ev.State == epRunning {
			ev.State = epPending
			ev.Error = ""
			ev.SpeedBps = 0
			ev.ETASeconds = 0
		}
	}
}

// drainEpisodeControls empties the job's buffered per-episode control channels.
// They are created once per job and survive across runs, so without a drain a
// pause key the previous run never consumed (e.g. clicked in the same instant
// the job was paused) would be delivered to the NEXT run's engine — silently
// holding an episode the card shows as pending/running, i.e. a download that
// hangs forever with no visible reason.
func drainEpisodeControls(j *Job) {
	for _, ch := range []chan domain.EpisodeKey{j.prioritize, j.pauseEp, j.resumeEp, j.retryEp, j.cancelEp} {
		for drained := false; !drained; {
			select {
			case <-ch:
			default:
				drained = true
			}
		}
	}
}

// resumeJob re-runs a paused job. The engine skips already-completed episodes and
// continues partial .hls-tmp segments, so the download picks up where it paused.
// Returns false if the job is not paused.
func (m *JobManager) resumeJob(id string) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	if j.status != statusPaused {
		j.mu.Unlock()
		return false
	}
	j.paused.Store(false)
	j.cancelRequested = false
	j.status = statusQueued
	j.errMsg = ""
	j.finishedAt = nil
	resetEpisodesForRerunLocked(j)
	j.mu.Unlock()
	drainEpisodeControls(j)

	// Hand back to the scheduler; it dispatches when a slot is free (or now).
	m.mu.Lock()
	m.pending = append(m.pending, j)
	m.mu.Unlock()
	m.publishNow(j)
	m.dispatch()
	return true
}

// pauseEpisode holds a single not-yet-started episode (pending or parked for
// retry) of a running job, setting it aside until resumed. Returns false if the
// job is not running or the episode is not in a holdable state (already
// downloading / completed / failed). The episode view is set to "paused"
// immediately for snappy feedback; the engine drains the request and skips it.
func (m *JobManager) pauseEpisode(id string, key domain.EpisodeKey) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	live := j.status == statusRunning || j.status == statusResolving
	ev := j.episodes[epKey(key)]
	// A queued, parked, or actively-downloading episode can be paused: the engine
	// holds queued/parked ones and cancels an in-flight download (preserving its
	// partial segments). Completed/failed/already-paused ones can't.
	pausable := ev != nil && (ev.State == epPending || ev.State == epDeferred || ev.State == epRunning)
	ch := j.pauseEp
	if live && pausable {
		ev.State = epPaused
		ev.SpeedBps = 0
		ev.ETASeconds = 0
	}
	j.mu.Unlock()
	if !live || !pausable {
		return false
	}
	select {
	case ch <- key:
	default:
	}
	m.publishNow(j)
	return true
}

// cancelEpisode drops a single episode from a RUNNING job: an in-flight download
// is stopped, a queued/parked/held one is pulled from the engine's queues — its
// siblings keep downloading and the run does NOT stay alive for it (unlike a
// pause). The row settles as failed/"canceled", so the per-episode Retry can
// bring it back later. Returns false if the job is not running or the episode
// is not in a cancelable state.
func (m *JobManager) cancelEpisode(id string, key domain.EpisodeKey) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	live := j.status == statusRunning || j.status == statusResolving
	ev := j.episodes[epKey(key)]
	cancelable := ev != nil &&
		(ev.State == epPending || ev.State == epDeferred || ev.State == epRunning || ev.State == epPaused)
	ch := j.cancelEp
	if live && cancelable {
		ev.State = epFailed
		ev.Error = "canceled"
		ev.SpeedBps = 0
		ev.ETASeconds = 0
	}
	j.mu.Unlock()
	if !live || !cancelable {
		return false
	}
	select {
	case ch <- key:
	default:
	}
	m.publishNow(j)
	return true
}

// resumeEpisode releases a paused episode back into a running job's work list.
// Returns false if the job is not running or the episode is not paused.
func (m *JobManager) resumeEpisode(id string, key domain.EpisodeKey) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	live := j.status == statusRunning || j.status == statusResolving
	ev := j.episodes[epKey(key)]
	resumable := ev != nil && ev.State == epPaused
	ch := j.resumeEp
	if live && resumable {
		ev.State = epPending // engine flips it to running when it actually starts
	}
	j.mu.Unlock()
	if !live || !resumable {
		return false
	}
	select {
	case ch <- key:
	default:
	}
	m.publishNow(j)
	return true
}

// retryEpisodeLive re-queues a single failed episode of a STILL-RUNNING job in
// place (no new job card): the engine re-attempts it among its siblings. Returns
// false if the job is not running. The episode view is optimistically reset.
func (m *JobManager) retryEpisodeLive(id string, key domain.EpisodeKey) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	live := j.status == statusRunning || j.status == statusResolving
	ev := j.episodes[epKey(key)]
	// Never live-retry an episode that already completed (would re-download and
	// double-count) or one already downloading; the engine guards this too.
	retriable := ev != nil && ev.State != epCompleted && ev.State != epRunning
	ch := j.retryEp
	if live && retriable {
		ev.State = epPending
		ev.Error = ""
		ev.Percent = 0
		ev.SpeedBps = 0
		ev.ETASeconds = 0
	}
	j.mu.Unlock()
	if !live || !retriable {
		return false
	}
	select {
	case ch <- key:
	default:
	}
	m.publishNow(j)
	return true
}

// rerunJob re-runs a FINISHED or PAUSED job in place (reusing the same card): it
// re-submits to the scheduler, and the engine skips episodes already completed
// in the state store while re-downloading the rest (failed/never-started). This
// is how a per-episode or whole-job retry avoids spawning a new job. Returns
// false if the job is still active (queued/running).
func (m *JobManager) rerunJob(id string) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	if !j.finished() && j.status != statusPaused {
		j.mu.Unlock()
		return false
	}
	j.paused.Store(false)
	j.cancelRequested = false
	j.status = statusQueued
	j.errMsg = ""
	j.finishedAt = nil
	j.summary = nil   // recomputed by the new run
	j.retryOnly = nil // whole-job retry: re-attempt every not-yet-completed episode
	resetEpisodesForRerunLocked(j)
	j.mu.Unlock()
	drainEpisodeControls(j)

	m.mu.Lock()
	m.pending = append(m.pending, j)
	m.mu.Unlock()
	m.publishNow(j)
	m.dispatch()
	return true
}

// rerunJobEpisode retries a SINGLE episode of a finished/paused job in place,
// re-downloading only that episode (not every not-yet-completed one). If a rerun
// is already pending (status queued), it widens that rerun's scope to include
// this episode too. Returns false if the job is actively running (use the live
// retry path instead).
func (m *JobManager) rerunJobEpisode(id string, key domain.EpisodeKey) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	resetRow := func() {
		if ev := j.episodes[epKey(key)]; ev != nil {
			ev.State = epPending
			ev.Error = ""
			ev.Percent = 0
			ev.SpeedBps = 0
			ev.ETASeconds = 0
		}
	}
	queue := false
	switch {
	case j.status == statusQueued:
		// A rerun is already pending — widen its scope to include this episode.
		if !containsKey(j.retryOnly, key) {
			j.retryOnly = append(j.retryOnly, key)
		}
		resetRow()
	case j.finished() || j.status == statusPaused:
		j.paused.Store(false)
		j.cancelRequested = false
		j.status = statusQueued
		j.errMsg = ""
		j.finishedAt = nil
		j.summary = nil
		j.retryOnly = []domain.EpisodeKey{key}
		resetRow()
		queue = true
	default:
		j.mu.Unlock()
		return false
	}
	j.mu.Unlock()

	if queue {
		// Fresh run for this retry: stale control keys from the previous run must
		// not leak into it (a leftover pause key would silently hold the episode).
		drainEpisodeControls(j)
		m.mu.Lock()
		m.pending = append(m.pending, j)
		m.mu.Unlock()
		m.dispatch()
	}
	m.publishNow(j)
	return true
}

// containsKey reports whether keys already holds an episode with the same
// season+episode.
func containsKey(keys []domain.EpisodeKey, k domain.EpisodeKey) bool {
	for _, e := range keys {
		if e.Season == k.Season && e.Episode == k.Episode {
			return true
		}
	}
	return false
}
