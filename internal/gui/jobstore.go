package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// persistedJob is the on-disk form of a Job: everything needed to restore its
// card after a restart AND to re-run it (cfg + seed metadata). Logs and the
// pending audio prompt are intentionally not persisted — logs would bloat the
// file, and an audio prompt cannot outlive the engine that asked it.
type persistedJob struct {
	ID         string            `json:"id"`
	URL        string            `json:"url"`
	Status     string            `json:"status"`
	Title      string            `json:"title,omitempty"`
	PosterURL  string            `json:"posterUrl,omitempty"`
	OutputPath string            `json:"outputPath,omitempty"`
	Quality    string            `json:"quality,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	Error      string            `json:"error,omitempty"`
	Plan       *PlanView         `json:"plan,omitempty"`
	Episodes   []EpisodeView     `json:"episodes,omitempty"`
	Summary    *SummaryView      `json:"summary,omitempty"`
	Titles     map[string]string `json:"titles,omitempty"`
	SeedTitles map[string]string `json:"seedTitles,omitempty"`
	Cfg        domain.RunConfig  `json:"cfg"`
}

// jobStore persists the job queue as JSON in the config dir, so unfinished
// downloads survive a restart and reappear as paused/failed cards that can be
// resumed (the engine skips completed episodes and continues .hls-tmp segments).
type jobStore struct {
	path string
}

func newJobStore() *jobStore {
	s := &jobStore{}
	if dir, err := configDir(); err == nil {
		s.path = filepath.Join(dir, "jobs.json")
	}
	return s
}

// load reads the persisted queue. A missing or unreadable file is an empty
// queue — restoring is always best-effort.
func (s *jobStore) load() []persistedJob {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	var jobs []persistedJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil
	}
	return jobs
}

// save atomically replaces the persisted queue (write temp + rename), so a
// crash mid-write can never corrupt the previous good snapshot.
func (s *jobStore) save(jobs []persistedJob) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// persistedFrom snapshots a Job into its on-disk form. Caller must NOT hold j.mu.
func persistedFrom(j *Job) persistedJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	eps := make([]EpisodeView, 0, len(j.episodes))
	for _, ev := range j.episodes {
		eps = append(eps, *ev)
	}
	titles := make(map[string]string, len(j.titles))
	for k, v := range j.titles {
		titles[k] = v
	}
	return persistedJob{
		ID:         j.id,
		URL:        j.url,
		Status:     j.status,
		Title:      j.title,
		PosterURL:  j.posterURL,
		OutputPath: j.outputPath,
		Quality:    j.quality,
		CreatedAt:  j.createdAt,
		StartedAt:  j.startedAt,
		FinishedAt: j.finishedAt,
		Error:      j.errMsg,
		Plan:       j.plan,
		Episodes:   eps,
		Summary:    j.summary,
		Titles:     titles,
		SeedTitles: j.seedTitles,
		Cfg:        j.cfg,
	}
}

// restoreJob rebuilds a live Job from its persisted form, normalizing state for
// a fresh process: anything that was in flight (queued/resolving/running) comes
// back as PAUSED — its engine died with the old process, but partial segments
// are still on disk, so Resume continues where it left off. Paused/failed/
// canceled/completed keep their status (failed keeps Retry).
func restoreJob(p persistedJob) *Job {
	j := newJob(p.ID, p.URL, p.Cfg)
	j.title = p.Title
	j.posterURL = p.PosterURL
	if p.OutputPath != "" {
		j.outputPath = p.OutputPath
	}
	if p.Quality != "" {
		j.quality = p.Quality
	}
	if !p.CreatedAt.IsZero() {
		j.createdAt = p.CreatedAt
	}
	j.startedAt = p.StartedAt
	j.finishedAt = p.FinishedAt
	j.errMsg = p.Error
	j.plan = p.Plan
	j.summary = p.Summary
	j.seedTitles = p.SeedTitles
	for k, v := range p.Titles {
		j.titles[k] = v
	}
	for i := range p.Episodes {
		ev := p.Episodes[i]
		ev.SpeedBps = 0
		ev.ETASeconds = 0
		j.episodes[ev.Key] = &ev
	}

	switch p.Status {
	case statusCompleted, statusFailed, statusCanceled, statusPaused:
		j.status = p.Status
	default:
		// queued / resolving / running — the run died with the old process.
		j.status = statusPaused
	}
	if j.status == statusPaused {
		j.paused.Store(true)
		settlePausedEpisodesLocked(j) // safe: j is not shared yet
		j.addLog(LogEntry{
			Time:    time.Now(),
			Level:   "INFO",
			Message: "restored after restart — press Resume to continue this download",
		})
	}
	return j
}
