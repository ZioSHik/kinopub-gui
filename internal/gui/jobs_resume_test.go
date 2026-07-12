package gui

import (
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// Resuming a paused job must immediately reset its stale paused/failed rows to
// pending — the engine's own reset only fires after resolve+plan, which can
// hang for minutes on a flaky VPN, leaving "paused" rows with live Resume
// buttons on a running job.
func TestResumeJobResetsStaleEpisodeRows(t *testing.T) {
	m := newJobManager(newHub())
	j := newJob("job-1", "u", domain.RunConfig{InputURL: "u"})
	j.status = statusPaused
	j.paused.Store(true)
	j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epPaused, Percent: 46, SpeedBps: 5}
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epFailed, Error: "boom"}
	j.episodes["S1E3"] = &EpisodeView{Key: "S1E3", Season: 1, Episode: 3, State: epCompleted, Percent: 100}
	m.add(j)

	// Stale control keys from the previous run must not leak into the next one.
	j.pauseEp <- domain.EpisodeKey{Season: 1, Episode: 1}
	j.prioritize <- domain.EpisodeKey{Season: 1, Episode: 2}

	if !m.resumeJob("job-1") {
		t.Fatal("resumeJob returned false for a paused job")
	}

	if st := j.episodes["S1E1"].State; st != epPending {
		t.Errorf("paused row after resume = %q, want %q", st, epPending)
	}
	if j.episodes["S1E1"].Percent != 46 {
		t.Errorf("progress must be kept on resume, got %d%%", j.episodes["S1E1"].Percent)
	}
	if j.episodes["S1E1"].SpeedBps != 0 {
		t.Error("stale speed must be cleared on resume")
	}
	if st := j.episodes["S1E2"].State; st != epPending {
		t.Errorf("failed row after resume = %q, want %q (the run re-attempts it)", st, epPending)
	}
	if j.episodes["S1E2"].Error != "" {
		t.Error("stale error must be cleared on resume")
	}
	if st := j.episodes["S1E3"].State; st != epCompleted {
		t.Errorf("completed row must stay completed, got %q", st)
	}
	select {
	case k := <-j.pauseEp:
		t.Errorf("stale pause key %v survived the resume drain — it would silently hold the episode", k)
	default:
	}
	select {
	case k := <-j.prioritize:
		t.Errorf("stale prioritize key %v survived the resume drain", k)
	default:
	}
}

// Retrying a finished job resets its failed rows to pending right away, for the
// same reason as resume (the engine reset only comes after resolve).
func TestRerunJobResetsFailedRows(t *testing.T) {
	m := newJobManager(newHub())
	j := newJob("job-1", "u", domain.RunConfig{InputURL: "u"})
	j.status = statusFailed
	j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epFailed, Error: "canceled"}
	m.add(j)

	if !m.rerunJob("job-1") {
		t.Fatal("rerunJob returned false for a failed job")
	}
	if st := j.episodes["S1E1"].State; st != epPending {
		t.Errorf("failed row after rerun = %q, want %q", st, epPending)
	}
}

// cancelEpisode drops one episode of a running job: optimistic view flips to
// failed/"canceled" and the key is delivered to the engine's cancel channel.
func TestCancelEpisode(t *testing.T) {
	m := newJobManager(newHub())
	j := newJob("job-1", "u", domain.RunConfig{InputURL: "u"})
	j.status = statusRunning
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epPending}
	m.add(j)

	key := domain.EpisodeKey{Season: 1, Episode: 2}
	if !m.cancelEpisode("job-1", key) {
		t.Fatal("cancelEpisode returned false for a pending episode of a running job")
	}
	ev := j.episodes["S1E2"]
	if ev.State != epFailed || ev.Error != "canceled" {
		t.Errorf("canceled row = %q/%q, want failed/canceled", ev.State, ev.Error)
	}
	select {
	case got := <-j.cancelEp:
		if got != key {
			t.Errorf("cancel channel got %v, want %v", got, key)
		}
	default:
		t.Error("cancel key was not delivered to the engine channel")
	}

	// A completed episode or a non-running job can't be canceled.
	ev.State = epCompleted
	if m.cancelEpisode("job-1", key) {
		t.Error("cancelEpisode must refuse a completed episode")
	}
	ev.State = epPending
	j.status = statusPaused
	if m.cancelEpisode("job-1", key) {
		t.Error("cancelEpisode must refuse when the job is not running")
	}
}
