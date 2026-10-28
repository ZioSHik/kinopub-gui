package gui

import (
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// activeJob builds a job that still owns its download, aimed at url/out.
func activeJob(id, url, out string, sel ...domain.EpisodeKey) *Job {
	j := newJob(id, url, domain.RunConfig{InputURL: url, OutputPath: out, SelectedEpisodes: sel})
	j.status = statusRunning
	return j
}

// queuedAt builds a paused card (what a restored queue is made of) created at a
// known time, so "oldest wins" is testable.
func queuedAt(id, url, out string, age time.Duration, sel ...domain.EpisodeKey) *Job {
	j := newJob(id, url, domain.RunConfig{InputURL: url, OutputPath: out, SelectedEpisodes: sel})
	j.status = statusPaused
	j.createdAt = time.Now().Add(-age)
	for _, k := range sel {
		key := epKey(k)
		j.episodes[key] = &EpisodeView{Key: key, Season: k.Season, Episode: k.Episode, State: epPending}
	}
	return j
}

func eps(from, to int) []domain.EpisodeKey {
	out := make([]domain.EpisodeKey, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, domain.EpisodeKey{Season: 1, Episode: i})
	}
	return out
}

func TestDedupeQueue(t *testing.T) {
	const url = "https://kino.watch/item/view/409"
	const out = "/downloads"

	t.Run("a card that adds nothing is dropped, the oldest kept", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(queuedAt("old", url, out, time.Hour, eps(1, 5)...))
		m.add(queuedAt("copy", url, out, time.Minute, eps(1, 5)...))
		if n := m.dedupeQueue(); n != 1 {
			t.Fatalf("dropped %d cards, want 1", n)
		}
		if _, ok := m.get("old"); !ok {
			t.Error("the original card was dropped")
		}
		if _, ok := m.get("copy"); ok {
			t.Error("the duplicate card survived")
		}
	})

	t.Run("a partial overlap is trimmed, not thrown away", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(queuedAt("old", url, out, time.Hour, eps(1, 10)...))
		m.add(queuedAt("newer", url, out, time.Minute, eps(5, 15)...))
		if n := m.dedupeQueue(); n != 0 {
			t.Fatalf("dropped %d cards, want 0 — episodes 11-15 are new work", n)
		}
		j, ok := m.get("newer")
		if !ok {
			t.Fatal("the card carrying new episodes was dropped")
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		if len(j.cfg.SelectedEpisodes) != 5 {
			t.Errorf("selection = %v, want episodes 11-15 only", j.cfg.SelectedEpisodes)
		}
		for _, k := range j.cfg.SelectedEpisodes {
			if k.Episode < 11 {
				t.Errorf("selection kept %v, which the older card is already downloading", k)
			}
		}
		if j.episodes["S1E7"] != nil {
			t.Error("an episode row survived for work handed back to the older card")
		}
		if j.episodes["S1E12"] == nil {
			t.Error("an episode row was lost for work this card still owns")
		}
	})

	t.Run("a movie queued twice collapses", func(t *testing.T) {
		// The shape a movie actually takes in a persisted queue: no explicit
		// selection (there are no episodes to pick), one resolved episode row per
		// card. Both cards paused after a restart, one part-way into its segments.
		m := newJobManager(newHub())
		older := queuedAt("old", url, out, time.Hour)
		older.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epPaused}
		newer := queuedAt("copy", url, out, time.Minute)
		newer.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epPending}
		m.add(older)
		m.add(newer)
		if n := m.dedupeQueue(); n != 1 {
			t.Fatalf("dropped %d cards, want 1", n)
		}
		if _, ok := m.get("copy"); ok {
			t.Error("the second card for the same movie survived")
		}
	})

	t.Run("cards that share nothing are both kept whole", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(queuedAt("a", url, out, time.Hour, eps(1, 5)...))
		m.add(queuedAt("b", url, out, time.Minute, eps(6, 10)...))
		if n := m.dedupeQueue(); n != 0 {
			t.Fatalf("dropped %d cards, want 0", n)
		}
		b, _ := m.get("b")
		b.mu.Lock()
		defer b.mu.Unlock()
		if len(b.cfg.SelectedEpisodes) != 5 {
			t.Errorf("selection = %v, want all five episodes untouched", b.cfg.SelectedEpisodes)
		}
	})

	t.Run("another title or folder is another download", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(queuedAt("a", url, out, time.Hour, eps(1, 5)...))
		m.add(queuedAt("b", "https://kino.watch/item/view/77", out, time.Minute, eps(1, 5)...))
		m.add(queuedAt("c", url, "/elsewhere", time.Second, eps(1, 5)...))
		if n := m.dedupeQueue(); n != 0 {
			t.Errorf("dropped %d cards, want 0", n)
		}
	})

	t.Run("two whole-title cards collapse into the older one", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(queuedAt("old", url, out, time.Hour))
		m.add(queuedAt("copy", url, out, time.Minute))
		if n := m.dedupeQueue(); n != 1 {
			t.Fatalf("dropped %d cards, want 1", n)
		}
		if _, ok := m.get("old"); !ok {
			t.Error("the original card was dropped")
		}
	})

	t.Run("finished cards are history and are left alone", func(t *testing.T) {
		for _, status := range []string{statusCompleted, statusFailed, statusCanceled} {
			m := newJobManager(newHub())
			done := queuedAt("done", url, out, time.Hour, eps(1, 5)...)
			done.status = status
			m.add(done)
			m.add(queuedAt("fresh", url, out, time.Minute, eps(1, 5)...))
			if n := m.dedupeQueue(); n != 0 {
				t.Errorf("status %q: dropped %d cards, want 0", status, n)
			}
		}
	})

	t.Run("a running card keeps its work and pushes the copy out", func(t *testing.T) {
		m := newJobManager(newHub())
		running := queuedAt("running", url, out, time.Minute, eps(1, 5)...)
		running.status = statusRunning
		// Deliberately OLDER than the running card: age must not let it claim
		// episodes out from under a download that is fetching them right now.
		m.add(queuedAt("waiting", url, out, time.Hour, eps(1, 5)...))
		m.add(running)
		if n := m.dedupeQueue(); n != 1 {
			t.Fatalf("dropped %d cards, want 1", n)
		}
		if _, ok := m.get("running"); !ok {
			t.Error("the running card was dropped")
		}
		if _, ok := m.get("waiting"); ok {
			t.Error("the idle copy of a running download survived")
		}
	})

	t.Run("a running card only claims what it is actually taking", func(t *testing.T) {
		m := newJobManager(newHub())
		running := queuedAt("running", url, out, time.Minute, eps(1, 5)...)
		running.status = statusRunning
		m.add(running)
		m.add(queuedAt("rest", url, out, time.Second, eps(1, 8)...))
		if n := m.dedupeQueue(); n != 0 {
			t.Fatalf("dropped %d cards, want 0 — episodes 6-8 are new work", n)
		}
		j, _ := m.get("rest")
		j.mu.Lock()
		defer j.mu.Unlock()
		if len(j.cfg.SelectedEpisodes) != 3 {
			t.Errorf("selection = %v, want episodes 6-8 only", j.cfg.SelectedEpisodes)
		}
	})
}

func TestQueueCoverage(t *testing.T) {
	const url = "https://kino.watch/item/view/409"
	const out = "/downloads"
	ask := domain.RunConfig{InputURL: url, OutputPath: out}

	t.Run("empty queue covers nothing", func(t *testing.T) {
		m := newJobManager(newHub())
		if _, _, _, found := m.queueCoverage(ask); found {
			t.Error("found = true on an empty queue")
		}
	})

	t.Run("an explicit selection covers exactly those episodes", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(activeJob("a", url, out, domain.EpisodeKey{Season: 1, Episode: 1}, domain.EpisodeKey{Season: 1, Episode: 2}))
		covered, whole, owner, found := m.queueCoverage(ask)
		if !found || whole {
			t.Fatalf("found = %v, whole = %v; want true, false", found, whole)
		}
		if owner.ID != "a" {
			t.Errorf("owner = %q, want the existing job", owner.ID)
		}
		if !covered["S1E1"] || !covered["S1E2"] || covered["S1E3"] {
			t.Errorf("covered = %v, want S1E1 and S1E2 only", covered)
		}
	})

	t.Run("an unresolved job with no selection owns the whole title", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(activeJob("a", url, out))
		if _, whole, _, found := m.queueCoverage(ask); !found || !whole {
			t.Errorf("found = %v, whole = %v; want true, true", found, whole)
		}
	})

	t.Run("a resolved plan wins over the selection, minus failures", func(t *testing.T) {
		m := newJobManager(newHub())
		j := activeJob("a", url, out, domain.EpisodeKey{Season: 1, Episode: 1})
		j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epRunning}
		j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epCompleted}
		// A failed episode left nothing to collide with: queueing it again is the
		// user's way out of the failure, so it must not be covered.
		j.episodes["S1E3"] = &EpisodeView{Key: "S1E3", Season: 1, Episode: 3, State: epFailed}
		m.add(j)
		covered, whole, _, found := m.queueCoverage(ask)
		if !found || whole {
			t.Fatalf("found = %v, whole = %v; want true, false", found, whole)
		}
		if !covered["S1E1"] || !covered["S1E2"] || covered["S1E3"] {
			t.Errorf("covered = %v, want the failed episode left out", covered)
		}
	})

	t.Run("finished jobs own nothing", func(t *testing.T) {
		for _, status := range []string{statusCompleted, statusFailed, statusCanceled} {
			m := newJobManager(newHub())
			j := activeJob("a", url, out, domain.EpisodeKey{Season: 1, Episode: 1})
			j.status = status
			m.add(j)
			if _, _, _, found := m.queueCoverage(ask); found {
				t.Errorf("status %q: found = true, want false", status)
			}
		}
	})

	t.Run("another title or another folder is another download", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(activeJob("a", "https://kino.watch/item/view/77", out, domain.EpisodeKey{Season: 1, Episode: 1}))
		m.add(activeJob("b", url, "/elsewhere", domain.EpisodeKey{Season: 1, Episode: 1}))
		if _, _, _, found := m.queueCoverage(ask); found {
			t.Error("found = true across a different title/folder")
		}
	})

	t.Run("dry runs write nothing and are ignored both ways", func(t *testing.T) {
		m := newJobManager(newHub())
		dry := newJob("a", url, domain.RunConfig{InputURL: url, OutputPath: out, DryRun: true})
		dry.status = statusRunning
		m.add(dry)
		if _, _, _, found := m.queueCoverage(ask); found {
			t.Error("a dry run blocked a real download")
		}
		m2 := newJobManager(newHub())
		m2.add(activeJob("a", url, out))
		asking := domain.RunConfig{InputURL: url, OutputPath: out, DryRun: true}
		if _, _, _, found := m2.queueCoverage(asking); found {
			t.Error("a real download blocked a dry run")
		}
	})

	t.Run("several active jobs are summed", func(t *testing.T) {
		m := newJobManager(newHub())
		m.add(activeJob("a", url, out, domain.EpisodeKey{Season: 1, Episode: 1}))
		m.add(activeJob("b", url, out, domain.EpisodeKey{Season: 2, Episode: 5}))
		covered, _, _, found := m.queueCoverage(ask)
		if !found || !covered["S1E1"] || !covered["S2E5"] {
			t.Errorf("covered = %v, want both jobs' episodes", covered)
		}
	})
}

func TestQueueKeepsDifferentRenditions(t *testing.T) {
	for _, change := range []func(*domain.RunConfig){
		func(c *domain.RunConfig) { c.Quality = "720p" },
		func(c *domain.RunConfig) { c.Container = domain.ContainerMP4 },
		func(c *domain.RunConfig) { c.AudioPref.Specs = []domain.AudioSpec{{Require: []string{"Studio B"}}} },
	} {
		m := newJobManager(newHub())
		old := queuedAt("old", "https://kino.watch/item/view/409", "/downloads", time.Hour, eps(1, 8)...)
		old.cfg.Quality = "1080p"
		old.cfg.AudioPref.Specs = []domain.AudioSpec{{Require: []string{"Studio A"}}}
		next := queuedAt("next", old.url, "/downloads", time.Minute, eps(1, 8)...)
		next.cfg = old.cfg
		change(&next.cfg)
		m.add(old)
		if _, ok := m.findActiveDuplicate(next.cfg); ok {
			t.Error("different rendition is a duplicate")
		}
		if _, _, _, ok := m.queueCoverage(next.cfg); ok {
			t.Error("different rendition claims new selection")
		}
		m.add(next)
		if n := m.dedupeQueue(); n != 0 {
			t.Errorf("cleanup dropped %d new renditions", n)
		}
	}
}
