package kinopub

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// trackHLS is an HLSDownloader that returns a fixed track list from
// ListAudioTracks and records the preference pushed via SetAudioPreference.
type trackHLS struct {
	tracks   []domain.AudioTrackInfo
	listErr  error
	gotPref  domain.AudioPreference
	prefSet  bool
	listCall int
}

func (h *trackHLS) DownloadEpisode(context.Context, string, domain.Quality, string, domain.EpisodeKey, domain.ProgressSink) (*domain.HLSDownloadResult, error) {
	return &domain.HLSDownloadResult{Resolution: "1280x720", BitrateKbps: 2000, Codec: "h264", VideoPath: "/tmp/v.ts"}, nil
}
func (h *trackHLS) ListAudioTracks(context.Context, string, domain.Quality) ([]domain.AudioTrackInfo, error) {
	h.listCall++
	if h.listErr != nil {
		return nil, h.listErr
	}
	return h.tracks, nil
}
func (h *trackHLS) SetAudioPreference(p domain.AudioPreference) {
	h.prefSet = true
	h.gotPref = p
}

// stubChooser returns predetermined indices (or an error).
type stubChooser struct {
	chosen []int
	err    error
	called bool
}

func (s *stubChooser) ChooseAudio([]domain.AudioTrackInfo, time.Duration) ([]int, error) {
	s.called = true
	return s.chosen, s.err
}

func selectedEpisodes(n int) []domain.Episode {
	var eps []domain.Episode
	for i := 1; i <= n; i++ {
		eps = append(eps, domain.Episode{Key: domain.EpisodeKey{Series: "42", Season: 1, Episode: i}})
	}
	return eps
}

func manifestMapFor(eps []domain.Episode) map[domain.EpisodeKey]string {
	m := make(map[domain.EpisodeKey]string)
	for _, ep := range eps {
		m[ep.Key] = "https://cdn/manifest.m3u8"
	}
	return m
}

// Fast path: no explicit pref and no menu → keep all, no probe.
func TestResolveAudioPreference_KeepAllNoProbe(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{{Index: 0, Name: "rus"}}}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	cfg := retryTestConfig() // AudioPref empty, AudioMenu false
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if !pref.IsAll() {
		t.Errorf("expected keep-all preference, got %+v", pref)
	}
	if h.listCall != 0 {
		t.Errorf("fast path must not probe tracks, ListAudioTracks called %d times", h.listCall)
	}
}

// Explicit pref with no Prefer hints gets enriched from probed tracks.
func TestResolveAudioPreference_ExplicitEnrichesPrefer(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{
		{Index: 0, Name: "AniLibria", Language: "rus"},
		{Index: 1, Name: "Original", Language: "jpn"},
	}}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	cfg := retryTestConfig()
	cfg.AudioPref = domain.AudioPreference{Include: []string{"anilibria"}}
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if !reflect.DeepEqual(pref.Include, []string{"anilibria"}) {
		t.Errorf("Include = %v, want [anilibria]", pref.Include)
	}
	// DeriveAudioPrefer should have produced ["rus"] from the matching track.
	if !reflect.DeepEqual(pref.Prefer, []string{"rus"}) {
		t.Errorf("Prefer = %v, want [rus] (derived from matched track)", pref.Prefer)
	}
}

// Explicit pref that already has Prefer hints is returned untouched.
func TestResolveAudioPreference_ExplicitKeepsExistingPrefer(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{{Index: 0, Name: "x", Language: "eng"}}}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	cfg := retryTestConfig()
	cfg.AudioPref = domain.AudioPreference{Include: []string{"foo"}, Prefer: []string{"ger"}}
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if !reflect.DeepEqual(pref.Prefer, []string{"ger"}) {
		t.Errorf("Prefer = %v, want [ger] (unchanged)", pref.Prefer)
	}
}

// Interactive menu with >1 track and a chooser returns a built preference.
func TestResolveAudioPreference_InteractiveMenu(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{
		{Index: 0, Name: "AniLibria", Language: "rus"},
		{Index: 1, Name: "Original", Language: "jpn"},
	}}
	chooser := &stubChooser{chosen: []int{0}}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.AudioChooser = chooser
	cfg := retryTestConfig()
	cfg.AudioMenu = true
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if !chooser.called {
		t.Fatal("chooser should have been called for the interactive menu")
	}
	if pref.IsAll() {
		t.Errorf("interactive selection should yield a non-empty preference, got keep-all")
	}
}

// Menu chooser error → keep all tracks.
func TestResolveAudioPreference_MenuErrorKeepsAll(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{
		{Index: 0, Name: "a", Language: "rus"},
		{Index: 1, Name: "b", Language: "jpn"},
	}}
	chooser := &stubChooser{err: errors.New("input closed")}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.AudioChooser = chooser
	cfg := retryTestConfig()
	cfg.AudioMenu = true
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if !pref.IsAll() {
		t.Errorf("chooser error should keep all tracks, got %+v", pref)
	}
}

// Menu chooser returning empty selection → keep all tracks.
func TestResolveAudioPreference_MenuEmptyChoiceKeepsAll(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{
		{Index: 0, Name: "a", Language: "rus"},
		{Index: 1, Name: "b", Language: "jpn"},
	}}
	chooser := &stubChooser{chosen: nil}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.AudioChooser = chooser
	cfg := retryTestConfig()
	cfg.AudioMenu = true
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if !pref.IsAll() {
		t.Errorf("empty choice should keep all tracks, got %+v", pref)
	}
}

// Menu enabled but only a single track → no prompt, keep all.
func TestResolveAudioPreference_MenuSingleTrackNoPrompt(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{{Index: 0, Name: "only", Language: "rus"}}}
	chooser := &stubChooser{chosen: []int{0}}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	e.deps.AudioChooser = chooser
	cfg := retryTestConfig()
	cfg.AudioMenu = true
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if chooser.called {
		t.Error("chooser must not be prompted when there is only one track")
	}
	if !pref.IsAll() {
		t.Errorf("single-track menu should keep all, got %+v", pref)
	}
}

// Probe failure (ListAudioTracks errors) with explicit pref → pref returned
// without crashing, no Prefer enrichment.
func TestResolveAudioPreference_ProbeFailureExplicit(t *testing.T) {
	h := &trackHLS{listErr: errors.New("network down")}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	cfg := retryTestConfig()
	cfg.AudioPref = domain.AudioPreference{Include: []string{"anilibria"}}
	eps := selectedEpisodes(1)
	pref := e.resolveAudioPreference(context.Background(), cfg, eps, manifestMapFor(eps))
	if !reflect.DeepEqual(pref.Include, []string{"anilibria"}) {
		t.Errorf("Include = %v, want [anilibria]", pref.Include)
	}
	if len(pref.Prefer) != 0 {
		t.Errorf("Prefer should be empty when probe fails, got %v", pref.Prefer)
	}
}

// Verify resolveAudioPreference is actually invoked by runHLS and its result is
// pushed to the downloader via SetAudioPreference.
func TestRunHLS_PushesAudioPreferenceToDownloader(t *testing.T) {
	h := &trackHLS{tracks: []domain.AudioTrackInfo{
		{Index: 0, Name: "AniLibria", Language: "rus"},
		{Index: 1, Name: "Original", Language: "jpn"},
	}}
	e, _, _ := newRetryTestEngine(h, &fakePageScraper{playlist: makePlaylist(1)})
	cfg := retryTestConfig()
	cfg.AudioPref = domain.AudioPreference{Include: []string{"anilibria"}}
	if _, err := e.runHLS(context.Background(), cfg); err != nil {
		t.Fatalf("runHLS error: %v", err)
	}
	if !h.prefSet {
		t.Fatal("SetAudioPreference was never called")
	}
	if !reflect.DeepEqual(h.gotPref.Include, []string{"anilibria"}) {
		t.Errorf("pushed Include = %v, want [anilibria]", h.gotPref.Include)
	}
}
