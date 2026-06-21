// Package statestore implements JSON-file-based download state persistence
// (Req 12). It records which episodes have been completed so that interrupted
// downloads can resume without re-downloading.
package statestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/fsutil"
)

const stateFileName = ".kinopub-state.json"

// JSONStore persists download state as a JSON file in the series download
// directory. It implements domain.StateStore.
//
// The state file lives inside the series folder (e.g.
// <output>/<Series Title>/.kinopub-state.json) so that each series has its own
// independent state. Call SetSeriesDir after the series title is known to
// activate the correct path.
//
// For backward compatibility, if the state file is not found in the series
// directory, Load falls back to reading from the root output directory (the
// legacy location). Writes always go to the series directory.
type JSONStore struct {
	rootDir   string // root output directory (e.g. -o flag value or cwd)
	seriesDir string // series subdirectory; empty until SetSeriesDir is called
	logger    domain.Logger
}

// New creates a JSONStore rooted at outputDir. Until SetSeriesDir is called,
// the state file is read from/written to outputDir (legacy behavior).
func New(outputDir string, logger domain.Logger) *JSONStore {
	return &JSONStore{
		rootDir: outputDir,
		logger:  logger.Component("statestore"),
	}
}

// SetSeriesDir sets the series-specific directory where the state file will be
// stored. This should be called after the series title is known (i.e. after
// feed parsing). The dir should be the full path to the series folder
// (e.g. <output>/<Series Title>).
func (s *JSONStore) SetSeriesDir(dir string) {
	s.seriesDir = dir
}

// statePath returns the full path to the state file. If seriesDir is set, the
// state file lives there; otherwise it falls back to rootDir.
func (s *JSONStore) statePath() string {
	if s.seriesDir != "" {
		return filepath.Join(s.seriesDir, stateFileName)
	}
	return filepath.Join(s.rootDir, stateFileName)
}

// legacyStatePath returns the path to the state file in the root output
// directory (the pre-migration location).
func (s *JSONStore) legacyStatePath() string {
	return filepath.Join(s.rootDir, stateFileName)
}

// Load reads the persisted state for the given series. If the file is missing,
// an empty state is returned (no error). If the file is corrupt or unreadable,
// an empty state is returned with a warn log (Req 12.5).
//
// When seriesDir is set and the state file is not found there, Load falls back
// to reading from the legacy root location for backward compatibility.
func (s *JSONStore) Load(_ context.Context, series domain.SeriesID) (domain.DownloadState, error) {
	empty := domain.DownloadState{
		Series:    series,
		Completed: make(map[string]domain.CompletedRec),
	}

	data, err := os.ReadFile(s.statePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Fall back to legacy location if seriesDir is set and differs from rootDir.
			if s.seriesDir != "" && s.statePath() != s.legacyStatePath() {
				legacyData, legacyErr := os.ReadFile(s.legacyStatePath())
				if legacyErr == nil {
					s.logger.Info("migrating state file from legacy location",
						domain.F("from", s.legacyStatePath()),
						domain.F("to", s.statePath()),
					)
					return s.parseLegacyState(legacyData, series)
				}
			}
			return empty, nil
		}
		s.logger.Warn("could not read state file; treating as empty state",
			domain.F("path", s.statePath()),
			domain.F("error", err.Error()),
		)
		return empty, nil
	}

	var state domain.DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		s.logger.Warn("state file is corrupt; treating as empty state",
			domain.F("path", s.statePath()),
			domain.F("error", err.Error()),
		)
		return empty, nil
	}

	// If the loaded state is for a different series, return empty.
	if state.Series != series {
		return empty, nil
	}

	// Ensure the map is never nil.
	if state.Completed == nil {
		state.Completed = make(map[string]domain.CompletedRec)
	}

	return state, nil
}

// parseLegacyState parses state data read from the legacy location.
func (s *JSONStore) parseLegacyState(data []byte, series domain.SeriesID) (domain.DownloadState, error) {
	empty := domain.DownloadState{
		Series:    series,
		Completed: make(map[string]domain.CompletedRec),
	}

	var state domain.DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		s.logger.Warn("legacy state file is corrupt; treating as empty state",
			domain.F("path", s.legacyStatePath()),
			domain.F("error", err.Error()),
		)
		return empty, nil
	}

	if state.Series != series {
		return empty, nil
	}

	if state.Completed == nil {
		state.Completed = make(map[string]domain.CompletedRec)
	}

	return state, nil
}

// episodeKeyString formats an EpisodeKey as the map key used in state JSON.
func episodeKeyString(key domain.EpisodeKey) string {
	return fmt.Sprintf("S%dE%d", key.Season, key.Episode)
}

// MarkCompleted persists a completed record for the given episode atomically
// (temp + rename via fsutil.AtomicWrite) before the job is recorded as
// succeeded (Req 12.1, 12.3).
func (s *JSONStore) MarkCompleted(_ context.Context, info domain.CompletedInfo) error {
	// Load current state (or start fresh).
	state, _ := s.Load(context.Background(), info.Key.Series)

	rec := domain.CompletedRec{
		Season:      info.Key.Season,
		Episode:     info.Key.Episode,
		Path:        info.Path,
		Bytes:       info.Bytes,
		CompletedAt: time.Now(),
		Title:       info.Title,
		Quality:     info.Quality,
		Resolution:  info.Resolution,
		BitRate:     info.BitRate,
		PageLink:    info.PageLink,
		MediaURL:    info.MediaURL,
	}

	state.Completed[episodeKeyString(info.Key)] = rec

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("statestore: marshal state: %w", err)
	}

	// Ensure the target directory exists (the series folder may not exist yet
	// on the very first write).
	stateDir := filepath.Dir(s.statePath())
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("statestore: create state dir: %w", err)
	}

	if err := fsutil.AtomicWrite(s.statePath(), data, 0644); err != nil {
		return fmt.Errorf("statestore: persist state: %w", err)
	}

	return nil
}

// SetMetadata persists series-level metadata (title, description, feed URL, etc.)
// into the state file for provenance and recovery.
func (s *JSONStore) SetMetadata(_ context.Context, series domain.SeriesID, meta domain.SeriesMetadata) error {
	state, _ := s.Load(context.Background(), series)
	state.Metadata = &meta

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("statestore: marshal state: %w", err)
	}

	// Ensure the target directory exists.
	stateDir := filepath.Dir(s.statePath())
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("statestore: create state dir: %w", err)
	}

	if err := fsutil.AtomicWrite(s.statePath(), data, 0644); err != nil {
		return fmt.Errorf("statestore: persist state: %w", err)
	}

	return nil
}

// IsCompleted checks whether the given episode is marked as completed in the
// loaded state (Req 12.2).
func (s *JSONStore) IsCompleted(state domain.DownloadState, key domain.EpisodeKey) bool {
	_, ok := state.Completed[episodeKeyString(key)]
	return ok
}

// Verify that *JSONStore satisfies domain.StateStore at compile time.
var _ domain.StateStore = (*JSONStore)(nil)
