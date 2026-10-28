package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestRunSignature_SameDownloadMatches(t *testing.T) {
	base := domain.RunConfig{
		InputURL:   "https://kino.watch/item/view/42",
		OutputPath: "/movies",
		Quality:    "1080p",
		SeasonSel:  domain.Selection{All: true},
		EpisodeSel: domain.Selection{All: true},
	}

	t.Run("another quality, container or voiceover is new work", func(t *testing.T) {
		for _, change := range []func(*domain.RunConfig){
			func(c *domain.RunConfig) { c.Quality = "480p" },
			func(c *domain.RunConfig) { c.Container = domain.ContainerMP4 },
			func(c *domain.RunConfig) { c.AudioPref.Specs = []domain.AudioSpec{{Require: []string{"lostfilm"}}} },
		} {
			other := base
			change(&other)
			if runSignature(base) == runSignature(other) {
				t.Error("another rendition was treated as a duplicate")
			}
		}
	})

	t.Run("output path is normalized", func(t *testing.T) {
		other := base
		other.OutputPath = "/movies/../movies/"
		if runSignature(base) != runSignature(other) {
			t.Error("equivalent paths must share a signature")
		}
	})

	t.Run("a different folder is a different download", func(t *testing.T) {
		other := base
		other.OutputPath = "/other"
		if runSignature(base) == runSignature(other) {
			t.Error("a different destination is not a duplicate")
		}
	})

	t.Run("a dry run does not collide with a real download", func(t *testing.T) {
		other := base
		other.DryRun = true
		if runSignature(base) == runSignature(other) {
			t.Error("a dry run writes nothing and must not block a real download")
		}
	})

	t.Run("episode selection order does not matter", func(t *testing.T) {
		a := base
		a.SelectedEpisodes = []domain.EpisodeKey{{Season: 1, Episode: 2}, {Season: 1, Episode: 1}}
		b := base
		b.SelectedEpisodes = []domain.EpisodeKey{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}}
		if runSignature(a) != runSignature(b) {
			t.Error("the same episodes picked in another order are the same download")
		}
		c := base
		c.SelectedEpisodes = []domain.EpisodeKey{{Season: 1, Episode: 1}}
		if runSignature(a) == runSignature(c) {
			t.Error("a different set of episodes is a different download")
		}
	})

	t.Run("season and episode expressions are compared by value", func(t *testing.T) {
		a := base
		a.SeasonSel = domain.Selection{Values: map[int]bool{2: true, 1: true}}
		b := base
		b.SeasonSel = domain.Selection{Values: map[int]bool{1: true, 2: true}}
		if runSignature(a) != runSignature(b) {
			t.Error("map iteration order must not leak into the signature")
		}
		c := base
		c.SeasonSel = domain.Selection{Values: map[int]bool{1: true}}
		if runSignature(a) == runSignature(c) {
			t.Error("a different season selection is a different download")
		}
	})
}

func TestFindActiveDuplicate(t *testing.T) {
	cfg := domain.RunConfig{InputURL: "https://kino.watch/item/view/42", OutputPath: "/movies"}
	m := newJobManager(newHub())

	if _, ok := m.findActiveDuplicate(cfg); ok {
		t.Fatal("an empty queue has no duplicates")
	}

	// Every state in which a job still owns its output folder blocks a second one.
	// A fresh manager per case: an active job cannot be removed from one.
	for _, st := range []string{statusQueued, statusResolving, statusRunning, statusPaused} {
		mgr := newJobManager(newHub())
		j := newJob("j-"+st, cfg.InputURL, cfg)
		j.status = st
		mgr.add(j)
		dup, ok := mgr.findActiveDuplicate(cfg)
		if !ok {
			t.Errorf("a %s job must be recognized as a duplicate", st)
		} else if dup.ID != j.id {
			t.Errorf("duplicate = %q, want %q", dup.ID, j.id)
		}
	}

	// Finished cards own nothing on disk — re-adding the title is allowed.
	for _, st := range []string{statusCompleted, statusFailed, statusCanceled} {
		j := newJob("done-"+st, cfg.InputURL, cfg)
		j.status = st
		m.add(j)
	}
	if _, ok := m.findActiveDuplicate(cfg); ok {
		t.Error("finished jobs must not block a new download")
	}

	// A running job for another title doesn't block this one.
	other := cfg
	other.InputURL = "https://kino.watch/item/view/99"
	oj := newJob("other", other.InputURL, other)
	oj.status = statusRunning
	m.add(oj)
	if _, ok := m.findActiveDuplicate(cfg); ok {
		t.Error("a different title must not count as a duplicate")
	}
}

func TestFindActiveDuplicate_ReportsTheOldestCard(t *testing.T) {
	cfg := domain.RunConfig{InputURL: "https://kino.watch/item/view/42"}
	m := newJobManager(newHub())
	older := newJob("older", cfg.InputURL, cfg)
	older.status = statusRunning
	older.createdAt = time.Now().Add(-time.Hour)
	newer := newJob("newer", cfg.InputURL, cfg)
	newer.status = statusQueued
	m.add(newer)
	m.add(older)

	dup, ok := m.findActiveDuplicate(cfg)
	if !ok || dup.ID != "older" {
		t.Errorf("want the original card 'older', got %q (found=%v)", dup.ID, ok)
	}
}

// The handler is the guard that matters: a double click on Download must not put
// two engines on one output folder.
func TestHandleCreateJob_RejectsDuplicate(t *testing.T) {
	s := newTestServer(t)
	body := map[string]any{
		"url":        "https://kino.watch/item/view/42",
		"outputPath": t.TempDir(),
		"quality":    "1080p",
		"dryRun":     true, // skips the ffmpeg precondition
	}
	raw, _ := json.Marshal(body)

	// Seed a running job built exactly the way the handler builds one.
	var req StartRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	cfg, err := buildRunConfig(req.RunRequest)
	if err != nil {
		t.Fatal(err)
	}
	seeded := newJob("job-1", cfg.InputURL, cfg)
	seeded.status = statusRunning
	s.mgr.add(seeded)

	w := httptest.NewRecorder()
	s.handleCreateJob(w, httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(raw)))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["jobId"] != "job-1" {
		t.Errorf("response should point at the existing job, got %v", resp)
	}
	if len(s.mgr.list()) != 1 {
		t.Errorf("no second job may be created, queue has %d", len(s.mgr.list()))
	}

	// The same request for a different folder is a different download → accepted.
	body["outputPath"] = t.TempDir()
	raw2, _ := json.Marshal(body)
	w2 := httptest.NewRecorder()
	s.handleCreateJob(w2, httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(raw2)))
	if w2.Code != http.StatusAccepted {
		t.Fatalf("different destination: status = %d, want %d (%s)", w2.Code, http.StatusAccepted, w2.Body.String())
	}
}

func TestHandleCreateJobAcceptsAnotherRenditionButRejectsSame(t *testing.T) {
	s := newTestServer(t)
	s.mgr.startFn = func(*Job) {} // exercise admission without external API/network
	req := StartRequest{RunRequest: RunRequest{URL: "https://kino.watch/item/view/409", OutputPath: t.TempDir(), Quality: "1080p", DryRun: true,
		AudioSpecs: []AudioSpecDTO{{Require: []string{"Studio A"}}}}}
	send := func() int {
		raw, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		s.handleCreateJob(w, httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(raw)))
		return w.Code
	}
	if status := send(); status != http.StatusAccepted {
		t.Fatalf("first job: %d", status)
	}
	if status := send(); status != http.StatusConflict {
		t.Fatalf("duplicate: %d", status)
	}
	req.AudioSpecs[0].Require = []string{"Studio B"}
	if status := send(); status != http.StatusAccepted {
		t.Fatalf("new voiceover: %d", status)
	}
	req.Quality = "720p"
	if status := send(); status != http.StatusAccepted {
		t.Fatalf("new resolution: %d", status)
	}
	if got := len(s.mgr.list()); got != 3 {
		t.Fatalf("jobs=%d, want three distinct requests", got)
	}
}
