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
	"sync"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
)

const stateFileName = ".kinopub-state.json"

// pathLocks serializes read-modify-write updates to each state file across
// JSONStore instances. Concurrent jobs for the same series — e.g. a per-episode
// retry running alongside its still-active parent job — each build their own
// JSONStore over the same file; with only the per-instance mutex their
// load→modify→write cycles could race and drop one job's completions. A shared
// lock keyed by the absolute state-file path makes those cycles mutually
// exclusive. Reads (Load) need no lock: writes land atomically via rename, so a
// reader always sees a complete old-or-new file.
//
// Entries are reference-counted and dropped once the last holder unlocks, so the
// table stays the size of what is actually being written rather than growing a
// permanent entry for every series ever downloaded in this process.
var pathLocks = struct {
	mu sync.Mutex
	m  map[string]*pathLock
}{m: make(map[string]*pathLock)}

type pathLock struct {
	mu   sync.Mutex
	refs int
}

// lockPath takes the shared lock for state-file path p and returns the function
// that releases it. The returned function must be called exactly once.
func lockPath(p string) func() {
	p = fsutil.CanonicalPath(p)
	pathLocks.mu.Lock()
	l := pathLocks.m[p]
	if l == nil {
		l = &pathLock{}
		pathLocks.m[p] = l
	}
	l.refs++
	pathLocks.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		pathLocks.mu.Lock()
		// The entry is only removed by the last holder, and a waiter has already
		// incremented refs before blocking on l.mu — so an entry can never be
		// dropped while another goroutine still holds a pointer to it.
		if l.refs--; l.refs == 0 {
			delete(pathLocks.m, p)
		}
		pathLocks.mu.Unlock()
	}
}

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
	// mu serializes state-file reads and read-modify-write updates so parallel
	// episode downloads (which each call MarkCompleted) cannot clobber each
	// other's completions or corrupt the JSON file.
	mu        sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(series)
}

// loadLocked is Load without acquiring s.mu; callers that already hold the lock
// (MarkCompleted, SetMetadata) use it to avoid a re-entrant deadlock.
func (s *JSONStore) loadLocked(series domain.SeriesID) (domain.DownloadState, error) {
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
	resolveCompletedPaths(&state, filepath.Dir(s.statePath()))

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
	resolveCompletedPaths(&state, s.rootDir)

	return state, nil
}

// Keep relative records anchored to the state file they came from, including
// a legacy state loaded from the output root before migrating to a series dir.
func resolveCompletedPaths(state *domain.DownloadState, dir string) {
	for key, rec := range state.Completed {
		files := rec.Files()
		for i := range files {
			if files[i].Path != "" && !filepath.IsAbs(files[i].Path) {
				files[i].Path = filepath.Join(dir, files[i].Path)
				if absolute, err := filepath.Abs(files[i].Path); err == nil {
					files[i].Path = absolute
				}
			}
		}
		rec = files[0]
		rec.Versions = files[1:]
		state.Completed[key] = rec
	}
}

// episodeKeyString formats an EpisodeKey as the map key used in state JSON.
func episodeKeyString(key domain.EpisodeKey) string {
	return fmt.Sprintf("S%dE%d", key.Season, key.Episode)
}

// MarkCompleted persists a completed record for the given episode atomically
// (temp + rename via fsutil.AtomicWrite) before the job is recorded as
// succeeded (Req 12.1, 12.3).
func (s *JSONStore) MarkCompleted(_ context.Context, info domain.CompletedInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize the load→modify→write across any other JSONStore writing the same
	// file, then load the freshest state under that lock so a sibling job's
	// completions are preserved (see pathLocks).
	unlock := lockPath(s.statePath())
	defer unlock()

	// Load current state (or start fresh).
	state, _ := s.loadLocked(info.Key.Series)
	// OutputLayout returns paths relative to the working directory when the
	// output root is relative. Persist an absolute path so reopening the state
	// never interprets that root a second time relative to the series folder.
	path := info.Path
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("statestore: resolve completed file: %w", err)
		}
		path = absolute
	}

	rec := domain.CompletedRec{
		RequestedQuality: info.RequestedQuality,
		AllAudio:         info.AllAudio,
		Season:           info.Key.Season,
		Episode:          info.Key.Episode,
		Path:             path,
		Bytes:            info.Bytes,
		CompletedAt:      time.Now(),
		Title:            info.Title,
		Quality:          info.Quality,
		Resolution:       info.Resolution,
		BitRate:          info.BitRate,
		PageLink:         info.PageLink,
		MediaURL:         info.MediaURL,

		Audio:         info.Audio,
		AudioFallback: info.AudioFallback,
	}

	if previous, ok := state.Completed[episodeKeyString(info.Key)]; ok {
		for _, file := range previous.Files() {
			if filepath.Clean(file.Path) != filepath.Clean(rec.Path) {
				rec.Versions = append(rec.Versions, file)
			}
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()

	unlock := lockPath(s.statePath())
	defer unlock()

	state, _ := s.loadLocked(series)
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

// LockedUpdate runs a read-modify-write of the state file at path under the
// same shared path lock every JSONStore write takes. Out-of-band writers — the
// GUI's metadata backfill, per-episode deletion from the library — must go
// through it: an unlocked load→modify→write racing a concurrent MarkCompleted
// would land last with a stale Completed map and silently erase a record the
// engine just wrote.
//
// mutate returns whether the (possibly modified) state should be written back;
// returning false leaves the file untouched. Read or parse failures are
// returned as-is without calling mutate.
func LockedUpdate(path string, mutate func(state *domain.DownloadState) (write bool, err error)) error {
	path = filepath.Clean(path)
	unlock := lockPath(path)
	defer unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var state domain.DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Completed == nil {
		state.Completed = make(map[string]domain.CompletedRec)
	}
	write, err := mutate(&state)
	if err != nil || !write {
		return err
	}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("statestore: marshal state: %w", err)
	}
	return fsutil.AtomicWrite(path, out, 0644)
}

// IsCompleted checks the record and its file. A stale record must not prevent
// restoring a movie or episode whose file was removed outside the app.
func (s *JSONStore) IsCompleted(state domain.DownloadState, key domain.EpisodeKey) bool {
	rec, ok := state.Completed[episodeKeyString(key)]
	if !ok {
		return false
	}
	s.mu.Lock()
	dir := filepath.Dir(s.statePath())
	s.mu.Unlock()
	for _, file := range rec.Files() {
		path := file.Path
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if fsutil.IsNonEmptyFile(path) {
			return true
		}
	}
	return false
}

// Verify that *JSONStore satisfies domain.StateStore at compile time.
var _ domain.StateStore = (*JSONStore)(nil)
