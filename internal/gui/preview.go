package gui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
	"github.com/ZioSHik/kinopub-gui/internal/services/statestore"
)

// PreviewEpisode is a single row in the series browser.
type PreviewEpisode struct {
	Key             string `json:"key"`
	Season          int    `json:"season"`
	Episode         int    `json:"episode"`
	Title           string `json:"title"`
	DurationSeconds int    `json:"durationSeconds"`
	Completed       bool   `json:"completed"`
	Selected        bool   `json:"selected"`
}

// PreviewSeason groups episodes by season.
type PreviewSeason struct {
	Number   int              `json:"number"`
	Episodes []PreviewEpisode `json:"episodes"`
}

// PreviewResponse is the resolved series catalog returned to the UI.
type PreviewResponse struct {
	SeriesID         string          `json:"seriesId"`
	Title            string          `json:"title"`
	OriginalTitle    string          `json:"originalTitle,omitempty"`
	Description      string          `json:"description,omitempty"`
	PosterURL        string          `json:"posterUrl,omitempty"`
	Seasons          []PreviewSeason `json:"seasons"`
	Total            int             `json:"total"`
	Selected         int             `json:"selected"`
	AlreadyCompleted int             `json:"alreadyCompleted"`
	Source           string          `json:"source"`
	Logs             []LogEntry      `json:"logs,omitempty"`
}

// preview resolves the series catalog for the browser/dry-run view via the
// official kino.pub API (an item URL → hls4 playlist).
func (s *Server) preview(ctx context.Context, req RunRequest) (*PreviewResponse, error) {
	cfg, err := buildRunConfig(req)
	if err != nil {
		return nil, err
	}
	if cfg.InputURL == "" {
		return nil, fmt.Errorf("a kino.pub URL is required")
	}

	logger, capture := newCaptureLogger(cfg.Verbosity)

	client, err := s.kpClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()

	playlist, err := kinopubapi.NewScraper(client, logger).ExtractAllSeasons(ctx, cfg.InputURL)
	if err != nil {
		return nil, err
	}
	series := seriesFromPlaylist(playlist)
	source := "api"

	// Load completion state from the series directory.
	state := loadSeriesState(ctx, cfg.OutputPath, series, logger)

	resp := &PreviewResponse{
		SeriesID:      string(series.ID),
		Title:         series.Title,
		OriginalTitle: series.OriginalTitle,
		Description:   series.Description,
		PosterURL:     series.PosterURL,
		Source:        source,
		Logs:          capture.entries,
	}

	for _, season := range series.Seasons {
		ps := PreviewSeason{Number: season.Number}
		for _, ep := range season.Episodes {
			selected := cfg.SeasonSel.Matches(season.Number) && cfg.EpisodeSel.Matches(ep.Key.Episode)
			completed := isCompleted(state, ep.Key)
			ps.Episodes = append(ps.Episodes, PreviewEpisode{
				Key:             epKey(ep.Key),
				Season:          ep.Key.Season,
				Episode:         ep.Key.Episode,
				Title:           ep.Title,
				DurationSeconds: int(ep.Duration / time.Second),
				Completed:       completed,
				Selected:        selected,
			})
			resp.Total++
			if completed {
				resp.AlreadyCompleted++
			}
			if selected && !completed {
				resp.Selected++
			}
		}
		resp.Seasons = append(resp.Seasons, ps)
	}
	sort.Slice(resp.Seasons, func(a, b int) bool { return resp.Seasons[a].Number < resp.Seasons[b].Number })
	resp.Title = firstNonEmpty(resp.Title, "Untitled")
	return resp, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// loadSeriesState points a fresh state store at the series directory and loads
// the completion state. Errors are swallowed (treated as "nothing completed").
func loadSeriesState(ctx context.Context, outputPath string, series domain.Series, logger domain.Logger) domain.DownloadState {
	store := statestore.New(outputPath, logger)
	store.SetSeriesDir(seriesDirPath(outputPath, series))
	state, err := store.Load(ctx, series.ID)
	if err != nil {
		return domain.DownloadState{Completed: map[string]domain.CompletedRec{}}
	}
	return state
}

func isCompleted(state domain.DownloadState, key domain.EpisodeKey) bool {
	if state.Completed == nil {
		return false
	}
	_, ok := state.Completed[epKey(key)]
	return ok
}

func seriesDirPath(root string, series domain.Series) string {
	fallback := fmt.Sprintf("series_%s", string(series.ID))
	return filepath.Join(root, fsutil.SanitizeComponent(series.Title, fallback))
}

// seriesFromPlaylist builds a domain.Series from a scraped page playlist
// (mirrors the engine's buildSeriesFromPlaylist).
func seriesFromPlaylist(playlist *domain.PagePlaylist) domain.Series {
	series := domain.Series{
		ID:        domain.SeriesID(fmt.Sprintf("%d", playlist.ItemID)),
		Title:     playlist.Title,
		PosterURL: playlist.Poster,
	}
	seasonMap := make(map[int][]domain.Episode)
	for _, pe := range playlist.Episodes {
		ep := domain.Episode{
			Key:      domain.EpisodeKey{Series: series.ID, Season: pe.Season, Episode: pe.Episode},
			Title:    pe.EpisodeTitle,
			Duration: time.Duration(pe.Duration) * time.Second,
		}
		seasonMap[pe.Season] = append(seasonMap[pe.Season], ep)
	}
	nums := make([]int, 0, len(seasonMap))
	for n := range seasonMap {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, sn := range nums {
		eps := seasonMap[sn]
		sort.Slice(eps, func(i, j int) bool { return eps[i].Key.Episode < eps[j].Key.Episode })
		series.Seasons = append(series.Seasons, domain.Season{Number: sn, Episodes: eps})
	}
	return series
}

// TitleMap flattens a series catalog into a "S{n}E{n}" → title lookup the UI can
// reuse to seed live job episode rows (sent back via StartRequest.SeedTitles).
func TitleMap(series domain.Series) map[string]string {
	m := make(map[string]string)
	for _, season := range series.Seasons {
		for _, ep := range season.Episodes {
			m[epKey(ep.Key)] = ep.Title
		}
	}
	return m
}
