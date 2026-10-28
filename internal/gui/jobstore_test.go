package gui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func testPersistedJob(id, status string) persistedJob {
	return persistedJob{
		ID:        id,
		URL:       "https://kino.watch/item/view/409",
		Status:    status,
		Title:     "Fear and Loathing",
		Quality:   "1080p",
		CreatedAt: time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC),
		Episodes: []EpisodeView{
			{Key: "S1E1", Season: 1, Episode: 1, State: epRunning, Percent: 18, Bytes: 2_800_000_000, Total: 15_000_000_000, TotalApprox: true, SpeedBps: 5_000_000, ETASeconds: 2278},
			{Key: "S1E2", Season: 1, Episode: 2, State: epCompleted, Percent: 100},
		},
		Titles: map[string]string{"S1E1": "Серия 1"},
		Cfg: domain.RunConfig{
			InputURL:   "https://kino.watch/item/view/409",
			OutputPath: "/tmp/out",
			Quality:    "1080p",
			UseAPI:     true,
			AudioPref:  domain.AudioPreference{Specs: []domain.AudioSpec{{Require: []string{"дубляж"}}}},
		},
	}
}

// A queue saved before the domain rename holds kino.pub links. They still
// resolve, but the card must show the current domain — restoring rewrites them,
// so the next save migrates the stored queue.
func TestRestoreJobCanonicalizesLegacyDomain(t *testing.T) {
	p := testPersistedJob("job-legacy", statusPaused)
	p.URL = "https://kino.pub/item/view/409"
	p.Cfg.InputURL = "https://kino.pub/item/view/409"

	j := restoreJob(p)

	const want = "https://kino.watch/item/view/409"
	if j.url != want {
		t.Errorf("job url = %q, want %q", j.url, want)
	}
	if j.cfg.InputURL != want {
		t.Errorf("cfg.InputURL = %q, want %q", j.cfg.InputURL, want)
	}
}

func TestJobStoreRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := newJobStore()

	in := []persistedJob{testPersistedJob("job-3", statusRunning)}
	if err := store.save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out := store.load()
	if len(out) != 1 {
		t.Fatalf("load: got %d jobs, want 1", len(out))
	}
	got := out[0]
	if got.ID != "job-3" || got.URL != in[0].URL || got.Quality != "1080p" {
		t.Errorf("identity fields lost: %+v", got)
	}
	if len(got.Episodes) != 2 || got.Episodes[0].Total != 15_000_000_000 || !got.Episodes[0].TotalApprox {
		t.Errorf("episode progress lost: %+v", got.Episodes)
	}
	if got.Cfg.OutputPath != "/tmp/out" || !got.Cfg.UseAPI {
		t.Errorf("cfg lost: %+v", got.Cfg)
	}
	if len(got.Cfg.AudioPref.Specs) != 1 || got.Cfg.AudioPref.Specs[0].Require[0] != "дубляж" {
		t.Errorf("audio spec lost: %+v", got.Cfg.AudioPref)
	}
}

func TestJobStoreLoadMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if jobs := newJobStore().load(); jobs != nil {
		t.Errorf("expected nil for missing file, got %v", jobs)
	}
}

func TestRestoreJobStatusMapping(t *testing.T) {
	cases := []struct {
		persisted string
		want      string
	}{
		{statusRunning, statusPaused},
		{statusResolving, statusPaused},
		{statusQueued, statusPaused},
		{statusPaused, statusPaused},
		{statusFailed, statusFailed},
		{statusCanceled, statusCanceled},
		{statusCompleted, statusCompleted},
	}
	for _, c := range cases {
		j := restoreJob(testPersistedJob("job-1", c.persisted))
		if j.status != c.want {
			t.Errorf("restore %q: got status %q, want %q", c.persisted, j.status, c.want)
		}
	}
}

func TestRestoreJobSettlesInterruptedEpisodes(t *testing.T) {
	j := restoreJob(testPersistedJob("job-1", statusRunning))

	ep := j.episodes["S1E1"]
	if ep == nil {
		t.Fatal("S1E1 not restored")
	}
	if ep.State != epPaused {
		t.Errorf("interrupted episode state = %q, want %q", ep.State, epPaused)
	}
	if ep.SpeedBps != 0 || ep.ETASeconds != 0 {
		t.Errorf("stale speed/ETA survived restore: %+v", ep)
	}
	if ep.Percent != 18 || ep.Bytes != 2_800_000_000 {
		t.Errorf("progress lost on restore: %+v", ep)
	}
	if done := j.episodes["S1E2"]; done == nil || done.State != epCompleted {
		t.Errorf("completed episode should stay completed: %+v", done)
	}
	if !j.paused.Load() {
		t.Error("restored paused job must have the paused flag set (so a resume run settles correctly)")
	}
	if j.titles["S1E1"] != "Серия 1" {
		t.Errorf("titles lost: %v", j.titles)
	}
}

func TestAttachStoreRestoresAndAdvancesSeq(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := newJobStore()
	if err := store.save([]persistedJob{
		testPersistedJob("job-7", statusRunning),
		testPersistedJob("job-2", statusFailed),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	m := newJobManager(newHub())
	m.attachStore(store)

	if _, ok := m.get("job-7"); !ok {
		t.Fatal("job-7 not restored")
	}
	if j, ok := m.get("job-2"); !ok || j.status != statusFailed {
		t.Fatal("job-2 not restored as failed")
	}
	// New ids must not collide with restored ones.
	if id := m.nextID(); id != "job-8" {
		t.Errorf("nextID = %q, want job-8", id)
	}
}

func TestPersistNowWritesAndSkipsDryRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := newJobStore()
	m := newJobManager(newHub())
	m.attachStore(store)

	real := newJob("job-1", "https://kino.watch/item/view/1", domain.RunConfig{InputURL: "https://kino.watch/item/view/1"})
	dry := newJob("job-2", "https://kino.watch/item/view/2", domain.RunConfig{InputURL: "https://kino.watch/item/view/2", DryRun: true})
	m.add(real)
	m.add(dry)
	m.markPersistDirty()
	m.persistNow()

	got := store.load()
	if len(got) != 1 || got[0].ID != "job-1" {
		t.Fatalf("persisted %v, want only job-1 (dry-run skipped)", got)
	}

	// Unchanged generation → no rewrite (file mtime/content stays as-is even if
	// the file is deleted, proving the gen gate short-circuits).
	os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "kinopub", "jobs.json"))
	m.persistNow()
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "kinopub", "jobs.json")); !os.IsNotExist(err) {
		t.Error("persistNow rewrote without a generation bump")
	}
}

func TestRemovePersistsSynchronously(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := newJobStore()
	m := newJobManager(newHub())
	m.attachStore(store)

	j := newJob("job-1", "u", domain.RunConfig{})
	j.status = statusFailed
	m.add(j)
	m.markPersistDirty()
	m.persistNow()
	if got := store.load(); len(got) != 1 {
		t.Fatalf("precondition: expected 1 persisted job, got %d", len(got))
	}

	if ok, _ := m.remove("job-1", false); !ok {
		t.Fatal("remove failed")
	}
	if got := store.load(); len(got) != 0 {
		t.Errorf("removed job still persisted: %v", got)
	}
}

func TestPersistRetriesFailedWriteWithoutAnotherJobChange(t *testing.T) {
	root := t.TempDir()
	obstacle := filepath.Join(root, "blocked")
	if err := os.WriteFile(obstacle, []byte("file blocks config directory"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newJobManager(newHub())
	m.store = &jobStore{path: filepath.Join(obstacle, "jobs.json")}
	m.add(newJob("job-1", "https://kino.watch/item/view/42", domain.RunConfig{}))
	m.markPersistDirty()
	m.persistNow()
	if m.persistedGen == m.persistGen.Load() {
		t.Fatal("failed write was marked persisted")
	}
	if err := os.Remove(obstacle); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(obstacle, 0700); err != nil {
		t.Fatal(err)
	}
	m.persistNow()
	if jobs := m.store.load(); len(jobs) != 1 {
		t.Fatalf("failed queue was not retried: %d jobs", len(jobs))
	}
}

func TestPersistedPlanDoesNotShareLiveSeasonMap(t *testing.T) {
	j := newJob("job-1", "42", domain.RunConfig{})
	j.plan = &PlanView{Total: 2, Seasons: map[int]int{1: 2}}
	saved := persistedFrom(j)
	j.plan.Seasons[1] = 3
	j.plan.Total = 3
	if saved.Plan.Total != 2 || saved.Plan.Seasons[1] != 2 {
		t.Fatal("persisted plan shares mutable live state")
	}
}
