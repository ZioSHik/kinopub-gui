package gui

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/app/kinopub"
	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// Job status values.
const (
	statusQueued    = "queued"
	statusResolving = "resolving"
	statusRunning   = "running"
	statusCompleted = "completed"
	statusFailed    = "failed"
	statusCanceled  = "canceled"
)

// Episode lifecycle states surfaced to the UI.
const (
	epPending   = "pending"
	epRunning   = "running"
	epCompleted = "completed"
	epFailed    = "failed"
	epDeferred  = "deferred"
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
	Key        string      `json:"key"`
	Season     int         `json:"season"`
	Episode    int         `json:"episode"`
	Title      string      `json:"title"`
	State      string      `json:"state"`
	Percent    int         `json:"percent"`
	Bytes      int64       `json:"bytes"`
	Total      int64       `json:"total"`
	SpeedBps   float64     `json:"speedBps"`
	ETASeconds int         `json:"etaSeconds"`
	SegDone    int         `json:"segDone"`
	SegTotal   int         `json:"segTotal"`
	Tracks     []TrackView `json:"tracks,omitempty"`
	Attempts   int         `json:"attempts"`
	Error      string      `json:"error,omitempty"`

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

	pendingAudio *AudioRequestView
	audioAnswer  chan []int // delivers the interactive audio selection
	logs         []LogEntry

	cancel context.CancelFunc
	done   <-chan struct{} // closed when the job's context is canceled/finished
	dirty  bool            // pending broadcast
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
}

func newJobManager(hub *Hub) *JobManager {
	m := &JobManager{
		jobs: make(map[string]*Job),
		hub:  hub,
	}
	go m.flushLoop()
	return m
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
}

// publishNow broadcasts a job immediately (used for important transitions).
func (m *JobManager) publishNow(j *Job) {
	j.mu.Lock()
	j.dirty = false
	view := j.snapshotLocked()
	j.mu.Unlock()
	m.hub.broadcast(Event{Type: "job", Data: view})
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
	if !j.finished() {
		m.mu.Unlock()
		return false, true // exists but running
	}
	delete(m.jobs, id)
	m.mu.Unlock()
	m.hub.broadcast(Event{Type: "job_removed", Data: map[string]string{"id": id}})
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
	return len(removed)
}

// run executes a download job end-to-end: it seeds episode metadata (best
// effort), wires the GUI reporter/chooser/logger, runs the engine, and records
// the outcome. It is meant to run in its own goroutine.
func (m *JobManager) run(parent context.Context, j *Job, cfg domain.RunConfig, titles map[string]string, title, poster string) {
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
	j.mu.Unlock()
	m.publishNow(j)

	reporter := newEventReporter(m, j)
	logger := newUILogger(m, j, cfg.Verbosity)

	var chooser domain.AudioChooser
	if cfg.AudioMenu {
		chooser = newGUIChooser(m, j)
	}

	deps, err := buildEngineDeps(cfg, logger, reporter, chooser)
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
	j.mu.Unlock()
	m.publishNow(j)
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

// cancel stops a running job.
func (m *JobManager) cancelJob(id string) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}
