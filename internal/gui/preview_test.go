package gui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "third"); got != "third" {
		t.Errorf("got %q, want third", got)
	}
	if got := firstNonEmpty("first", "second"); got != "first" {
		t.Errorf("got %q, want first", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("got %q, want empty for no args", got)
	}
}

func TestIsCompleted(t *testing.T) {
	state := domain.DownloadState{Completed: map[string]domain.CompletedRec{"S1E1": {}}}
	if !isCompleted(state, domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Error("S1E1 should be completed")
	}
	if isCompleted(state, domain.EpisodeKey{Season: 1, Episode: 2}) {
		t.Error("S1E2 should not be completed")
	}
	// Nil map → not completed (no panic).
	if isCompleted(domain.DownloadState{}, domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Error("nil completed map should report not-completed")
	}
}

func TestSeriesDirPath(t *testing.T) {
	got := seriesDirPath("/root", domain.Series{ID: "42", Title: "My Show"})
	want := filepath.Join("/root", "My Show")
	if got != want {
		t.Errorf("seriesDirPath = %q, want %q", got, want)
	}
	// Empty title falls back to series_<id>.
	got = seriesDirPath("/root", domain.Series{ID: "42", Title: ""})
	want = filepath.Join("/root", "series_42")
	if got != want {
		t.Errorf("seriesDirPath(empty title) = %q, want %q", got, want)
	}
}

func TestSeriesFromPlaylist(t *testing.T) {
	pl := &domain.PagePlaylist{
		ItemID: 99,
		Title:  "Show",
		Poster: "p.jpg",
		Episodes: []domain.PageEpisode{
			{Season: 2, Episode: 1, EpisodeTitle: "S2E1", Duration: 60},
			{Season: 1, Episode: 2, EpisodeTitle: "S1E2", Duration: 120},
			{Season: 1, Episode: 1, EpisodeTitle: "S1E1", Duration: 90},
		},
	}
	series := seriesFromPlaylist(pl)
	if string(series.ID) != "99" || series.Title != "Show" || series.PosterURL != "p.jpg" {
		t.Errorf("series header wrong: %+v", series)
	}
	// Seasons sorted ascending; episodes within a season sorted by episode number.
	if len(series.Seasons) != 2 {
		t.Fatalf("want 2 seasons, got %d", len(series.Seasons))
	}
	if series.Seasons[0].Number != 1 || series.Seasons[1].Number != 2 {
		t.Errorf("seasons not sorted: %+v", series.Seasons)
	}
	s1 := series.Seasons[0]
	if s1.Episodes[0].Key.Episode != 1 || s1.Episodes[1].Key.Episode != 2 {
		t.Errorf("episodes not sorted: %+v", s1.Episodes)
	}
	if s1.Episodes[0].Duration != 90*time.Second {
		t.Errorf("duration not converted to seconds, got %v", s1.Episodes[0].Duration)
	}
}

func TestTitleMap(t *testing.T) {
	series := domain.Series{
		ID: "1",
		Seasons: []domain.Season{
			{Number: 1, Episodes: []domain.Episode{
				{Key: domain.EpisodeKey{Season: 1, Episode: 1}, Title: "Pilot"},
				{Key: domain.EpisodeKey{Season: 1, Episode: 2}, Title: "Second"},
			}},
		},
	}
	m := TitleMap(series)
	if m["S1E1"] != "Pilot" || m["S1E2"] != "Second" {
		t.Errorf("TitleMap = %v", m)
	}
	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m))
	}
}
