package kinopub

import (
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func sampleSeries() domain.Series {
	return domain.Series{
		ID: "1",
		Seasons: []domain.Season{
			{Number: 1, Episodes: []domain.Episode{
				{Key: domain.EpisodeKey{Series: "1", Season: 1, Episode: 1}},
				{Key: domain.EpisodeKey{Series: "1", Season: 1, Episode: 2}},
			}},
			{Number: 2, Episodes: []domain.Episode{
				{Key: domain.EpisodeKey{Series: "1", Season: 2, Episode: 1}},
			}},
		},
	}
}

func TestMatchingEpisodes_ExplicitSelectionWins(t *testing.T) {
	e := &engine{}
	series := sampleSeries()

	// Explicit selection of exactly S1E2 must ignore the (default-all) SeasonSel
	// / EpisodeSel and return only that episode — this is the fix for "preview
	// shows N episodes but only 1 downloads".
	cfg := domain.RunConfig{
		SeasonSel:        domain.Selection{All: true},
		EpisodeSel:       domain.Selection{All: true},
		SelectedEpisodes: []domain.EpisodeKey{{Season: 1, Episode: 2}},
	}
	got := e.matchingEpisodes(series, cfg)
	if len(got) != 1 || got[0].Key.Season != 1 || got[0].Key.Episode != 2 {
		t.Fatalf("explicit selection = %+v, want only S1E2", got)
	}
}

func TestMatchingEpisodes_FallsBackToSelectionWhenNoExplicit(t *testing.T) {
	e := &engine{}
	series := sampleSeries()
	cfg := domain.RunConfig{
		SeasonSel:  domain.Selection{All: true},
		EpisodeSel: domain.Selection{All: true},
	}
	if got := e.matchingEpisodes(series, cfg); len(got) != 3 {
		t.Fatalf("default selection = %d episodes, want 3", len(got))
	}
}
