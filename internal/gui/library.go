package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
	"github.com/ZioSHik/kinopub-gui/internal/lib/mediainfo"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
	"github.com/ZioSHik/kinopub-gui/internal/services/statestore"
)

const stateFileName = ".kinopub-state.json"

// LibraryEpisode is one completed episode recorded in a state file.
type LibraryEpisode struct {
	Versions         []LibraryEpisode `json:"versions,omitempty"`
	RequestedQuality string           `json:"requestedQuality,omitempty"`
	Quality          string           `json:"quality,omitempty"`
	AllAudio         bool             `json:"allAudio,omitempty"`
	Key              string           `json:"key"`
	Season           int              `json:"season"`
	Episode          int              `json:"episode"`
	Title            string           `json:"title"`
	Path             string           `json:"path"`
	Exists           bool             `json:"exists"`
	Bytes            int64            `json:"bytes"`
	Resolution       string           `json:"resolution,omitempty"`
	CompletedAt      time.Time        `json:"completedAt"`
	// Audio is the voiceover this episode was downloaded with, and AudioFallback
	// marks it as a substitute taken because the requested one was not offered.
	Audio         []string `json:"audio,omitempty"`
	AudioFallback bool     `json:"audioFallback,omitempty"`
}

// LibrarySeries aggregates one series' completed downloads.
type LibrarySeries struct {
	Dir           string           `json:"dir"`
	StateFile     string           `json:"stateFile"`
	SeriesID      string           `json:"seriesId"`
	Title         string           `json:"title"`
	OriginalTitle string           `json:"originalTitle,omitempty"`
	Description   string           `json:"description,omitempty"`
	PosterURL     string           `json:"posterUrl,omitempty"`
	InputURL      string           `json:"inputUrl,omitempty"`
	Type          string           `json:"type,omitempty"`   // kino.watch item type (movie, serial, …)
	IsMovie       bool             `json:"isMovie"`          // movie vs series, for the library split
	Genres        []string         `json:"genres,omitempty"` // genre titles, for filtering
	Count         int              `json:"count"`
	TotalBytes    int64            `json:"totalBytes"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	Episodes      []LibraryEpisode `json:"episodes"`
}

// LibraryResponse is the scan result returned to the UI.
type LibraryResponse struct {
	Series []LibrarySeries `json:"series"`
	Dirs   []string        `json:"dirs"`
}

// DownloadedEpisode is one already-downloaded episode of a kino.watch item, used
// to mark which episodes the title card already has on disk.
type DownloadedEpisode struct {
	Path             string `json:"path,omitempty"`
	RequestedQuality string `json:"requestedQuality,omitempty"`
	Quality          string `json:"quality,omitempty"`
	AllAudio         bool   `json:"allAudio,omitempty"`
	Key              string `json:"key"`
	Season           int    `json:"season"`
	Episode          int    `json:"episode"`
	Resolution       string `json:"resolution,omitempty"`
	Exists           bool   `json:"exists"`
	// What the episode actually came out in, so the card can show the voiceover
	// on disk rather than the last one the user happened to pick elsewhere.
	Audio         []string `json:"audio,omitempty"`
	AudioFallback bool     `json:"audioFallback,omitempty"`
}

// DownloadedResponse lists the episodes of a kino.watch item already downloaded.
type DownloadedResponse struct {
	ID       string              `json:"id"`
	Dir      string              `json:"dir,omitempty"`
	Episodes []DownloadedEpisode `json:"episodes"`
}

// downloadedForItem scans the library roots for downloads belonging to the given
// kino.watch item id — matched on the recorded series id or, failing that, the id
// embedded in the saved InputURL — and returns the episodes already on disk.
func downloadedForItem(dirs []string, itemID string, outputRoot ...string) DownloadedResponse {
	resp := DownloadedResponse{ID: itemID, Episodes: []DownloadedEpisode{}}
	if itemID == "" {
		return resp
	}
	for _, series := range scanLibrary(dirs).Series {
		if len(outputRoot) > 0 && outputRoot[0] != "" && canonicalOutput(filepath.Dir(series.Dir)) != canonicalOutput(outputRoot[0]) {
			continue
		}
		if !seriesMatchesItem(series, itemID) {
			continue
		}
		if resp.Dir == "" {
			resp.Dir = series.Dir
		}
		for _, primary := range series.Episodes {
			for _, ep := range append([]LibraryEpisode{primary}, primary.Versions...) {
				resp.Episodes = append(resp.Episodes, DownloadedEpisode{
					Path: ep.Path, RequestedQuality: ep.RequestedQuality, Quality: ep.Quality, AllAudio: ep.AllAudio,
					Key:        ep.Key,
					Season:     ep.Season,
					Episode:    ep.Episode,
					Resolution: ep.Resolution,
					Exists:     ep.Exists,

					Audio:         ep.Audio,
					AudioFallback: ep.AudioFallback,
				})
			}
		}
	}
	return resp
}

// seriesMatchesItem reports whether a scanned series belongs to the kino.watch
// item id.
func seriesMatchesItem(s LibrarySeries, itemID string) bool {
	if s.SeriesID == itemID {
		return true
	}
	return s.InputURL != "" && kinopubapi.ItemIDFromURL(s.InputURL) == itemID
}

// scanLibrary walks the given directories looking for kinopub state files and
// builds a catalog of completed downloads.
func scanLibrary(dirs []string) LibraryResponse {
	if dirs == nil {
		dirs = []string{}
	}
	// Always return non-nil slices so the JSON is [] (not null), which the UI
	// can safely .map/.filter over.
	resp := LibraryResponse{Dirs: dirs, Series: []LibrarySeries{}}
	seen := make(map[string]bool)

	for _, root := range dirs {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				// Skip hidden directories (but not the root itself).
				if path != root && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() != stateFileName {
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			if item, ok := readLibraryState(path); ok {
				resp.Series = append(resp.Series, item)
			}
			return nil
		})
	}

	// Recover audio from older downloads whose state predates audio recording.
	// A shared deadline bounds disk work for a large library; cached results remain
	// available on later scans without running ffprobe again.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := range resp.Series {
		for j := range resp.Series[i].Episodes {
			ep := &resp.Series[i].Episodes[j]
			recoverLibraryAudio(ctx, ep)
			for k := range ep.Versions {
				recoverLibraryAudio(ctx, &ep.Versions[k])
			}
		}
	}

	sort.Slice(resp.Series, func(a, b int) bool {
		return resp.Series[a].UpdatedAt.After(resp.Series[b].UpdatedAt)
	})
	return resp
}

// maxMetadataBackfills bounds how many API lookups one library scan may spend
// enriching old entries, so a large library of pre-metadata downloads can't turn
// a rescan into a long stall.
const maxMetadataBackfills = 12

// backfillLibraryMetadata fills in descriptive fields that were not recorded
// when an entry was downloaded. Genres and the item type only started being
// written to state files in later versions, so anything downloaded before that
// carries neither and its library card has nothing to show. Missing fields are
// fetched from the API and written back into the state file, so the lookup cost
// is paid once rather than on every scan.
//
// It takes copies of the entries rather than the live scan result: this runs off
// the request goroutine, and mutating what the handler is already marshalling
// would be a data race. The enrichment lands in the state files, so the next scan
// serves it from disk.
//
// This is best-effort: a failed lookup or an unwritable state file leaves the
// entry exactly as it was, and the scan the user already got still stands.
func backfillLibraryMetadata(ctx context.Context, entries []LibrarySeries, client *kinopubapi.Client) {
	spent := 0
	for _, s := range entries {
		if spent >= maxMetadataBackfills {
			return
		}
		if len(s.Genres) > 0 && s.Type != "" {
			continue
		}
		id := libraryItemID(s)
		if id == "" {
			continue
		}
		spent++
		item, err := client.Item(ctx, id)
		if err != nil {
			// Give up on the first failure. These calls share one API client
			// whose mutex serializes every other request behind them, so when
			// kino.watch is unreachable — the normal case without a VPN —
			// grinding through the rest would hold the whole app hostage for
			// one timeout after another.
			return
		}
		genres := s.Genres
		typ := s.Type
		if len(genres) == 0 {
			genres = titleNames(item.Genres)
		}
		if typ == "" {
			typ = item.Type
		}
		if len(genres) == 0 && typ == "" {
			continue // the API had nothing to add either
		}
		_ = persistLibraryMetadata(s.StateFile, genres, typ)
	}
}

// needsMetadataBackfill returns copies of the entries missing genres or type.
// Copies, not pointers: see backfillLibraryMetadata.
func needsMetadataBackfill(series []LibrarySeries) []LibrarySeries {
	var out []LibrarySeries
	for _, s := range series {
		if len(s.Genres) == 0 || s.Type == "" {
			out = append(out, s)
		}
	}
	return out
}

// libraryItemID resolves the kino.watch item id for a scanned entry, preferring
// the recorded input URL over the series id.
func libraryItemID(s LibrarySeries) string {
	if s.InputURL != "" {
		if id := kinopubapi.ItemIDFromURL(s.InputURL); id != "" {
			return id
		}
	}
	return s.SeriesID
}

// persistLibraryMetadata writes recovered genres and type back into a state
// file, leaving every other field — including UpdatedAt, which orders the
// library — untouched.
//
// It goes through statestore.LockedUpdate: this runs on the background backfill
// goroutine while an active download for the same series may be recording
// completions through JSONStore.MarkCompleted, and an unlocked read-modify-write
// here could land last with a stale Completed map, erasing an episode the
// engine just finished.
func persistLibraryMetadata(stateFile string, genres []string, typ string) error {
	return statestore.LockedUpdate(stateFile, func(state *domain.DownloadState) (bool, error) {
		if state.Metadata == nil {
			// Nothing to attach to: such an entry has no recorded title or poster
			// either, and inventing a metadata block here would be guesswork.
			return false, nil
		}
		if len(state.Metadata.Genres) == 0 {
			state.Metadata.Genres = genres
		}
		if state.Metadata.Type == "" {
			state.Metadata.Type = typ
		}
		return true, nil
	})
}

// resolveLibraryDir validates that dir is a real kinopub download folder safe to
// modify: it must (a) contain a kinopub state file and (b) live strictly inside
// one of the configured library/output roots — never a root itself or an
// arbitrary path. It returns the cleaned absolute path.
func resolveLibraryDir(dir string, roots []string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(filepath.Join(abs, stateFileName)); err != nil {
		return "", fmt.Errorf("not a kinopub download folder (no %s)", stateFileName)
	}

	for _, root := range roots {
		rabs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rabs, err = filepath.EvalSymlinks(rabs)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(rabs), abs)
		if err != nil {
			continue
		}
		// Strictly inside: not "." (the root itself) and not escaping with "..".
		if rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("folder is outside the configured library folders")
}

// deleteLibrarySeries removes a downloaded series directory (its files and state
// file) from disk, after validating it is a kinopub download folder inside a
// configured root.
func deleteLibrarySeries(dir string, roots []string) error {
	abs, err := resolveLibraryDir(dir, roots)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

// deleteLibraryEpisode removes a single downloaded episode's file from disk and
// drops its record from the series state file, so a watched episode stops taking
// up space without discarding the rest of the series. When the deleted episode
// was the last one, the whole series folder is removed.
func deleteLibraryEpisode(dir, key string, roots []string) error {
	abs, err := resolveLibraryDir(dir, roots)
	if err != nil {
		return err
	}
	stateFile := filepath.Join(abs, stateFileName)
	// The whole read-check-delete-write runs under the statestore path lock, so
	// a download recording a completion into the same file at the same moment
	// can neither be lost nor resurrect the record being deleted here.
	err = statestore.LockedUpdate(stateFile, func(state *domain.DownloadState) (bool, error) {
		rec, ok := state.Completed[key]
		if !ok {
			return false, fmt.Errorf("episode %q not found in this download", key)
		}

		// Validate every version before deleting any. Resolve symlinked parents too,
		// so a state record cannot remove a file outside the series via a directory link.
		var paths []string
		for _, file := range rec.Files() {
			if file.Path == "" {
				continue
			}
			full := file.Path
			if !filepath.IsAbs(full) {
				full = filepath.Join(abs, full)
			}
			parent, err := filepath.EvalSymlinks(filepath.Dir(full))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return false, err
			}
			clean := filepath.Join(parent, filepath.Base(full))
			rel, err := filepath.Rel(abs, clean)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return false, fmt.Errorf("episode file is outside its series folder")
			}
			paths = append(paths, clean)
		}
		for _, path := range paths {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, fmt.Errorf("remove episode file: %w", err)
			}
		}

		delete(state.Completed, key)

		return true, nil
	})
	if err != nil {
		return err
	}
	// Keep the empty state file: scans omit it, while a concurrent completion can
	// still safely write to this folder. Never remove unrelated user files.

	return nil
}

// isSerialType reports whether a kino.watch item type denotes a series (serial,
// docuserial, tvshow) rather than a movie.
func isSerialType(t string) bool {
	t = strings.ToLower(t)
	return strings.Contains(t, "serial") || strings.Contains(t, "show")
}

// isMovieDownload classifies a scanned download as a movie or a series. New
// downloads carry the kino.watch item type; for older ones (recorded before the
// type was persisted) it falls back to a structural heuristic — a single part
// in a single season looks like a movie.
func isMovieDownload(s LibrarySeries) bool {
	if s.Type != "" {
		return !isSerialType(s.Type)
	}
	seasons := make(map[int]bool, 2)
	for _, ep := range s.Episodes {
		seasons[ep.Season] = true
	}
	return len(seasons) <= 1 && len(s.Episodes) <= 1
}

func readLibraryState(stateFile string) (LibrarySeries, bool) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return LibrarySeries{}, false
	}
	var state domain.DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		return LibrarySeries{}, false
	}
	// A state file with no completed episode records nothing the user has: a run
	// that resolved and then downloaded nothing (failed, canceled, stopped before
	// the first episode) used to leave a metadata-only file behind. The library
	// scans for state files, so such a file became a phantom card for a title that
	// was never downloaded — and, having zero episodes, the fallback in
	// isMovieDownload filed it under "movies". Newer runs no longer write metadata
	// before the first completion, but old folders (and hand-emptied ones) still
	// carry these files, so the scan skips them here too.
	if len(state.Completed) == 0 {
		return LibrarySeries{}, false
	}
	dir := filepath.Dir(stateFile)
	item := LibrarySeries{
		Dir:       dir,
		StateFile: stateFile,
		SeriesID:  string(state.Series),
		Title:     filepath.Base(dir),
	}
	if state.Metadata != nil {
		if state.Metadata.Title != "" {
			item.Title = state.Metadata.Title
		}
		item.OriginalTitle = state.Metadata.OriginalTitle
		item.Description = state.Metadata.Description
		item.PosterURL = state.Metadata.PosterURL
		item.InputURL = state.Metadata.InputURL
		item.Type = state.Metadata.Type
		item.Genres = state.Metadata.Genres
		item.UpdatedAt = state.Metadata.UpdatedAt
	}

	for key, rec := range state.Completed {
		files := rec.Files()
		var versions []LibraryEpisode
		for _, file := range files {
			ep := libraryFile(key, file, dir)
			versions = append(versions, ep)
			if ep.Exists {
				item.TotalBytes += ep.Bytes
			}
		}
		primary := versions[0]
		// Prefer a playable version when the most recently recorded file was removed.
		if !primary.Exists {
			for i := 1; i < len(versions); i++ {
				if versions[i].Exists {
					versions[0], versions[i] = versions[i], versions[0]
					primary = versions[0]
					break
				}
			}
		}
		primary.Versions = versions[1:]
		item.Episodes = append(item.Episodes, primary)

		if rec.CompletedAt.After(item.UpdatedAt) {
			item.UpdatedAt = rec.CompletedAt
		}
	}
	item.Count = len(item.Episodes)
	if item.Episodes == nil {
		item.Episodes = []LibraryEpisode{}
	}
	item.IsMovie = isMovieDownload(item)
	sort.Slice(item.Episodes, func(a, b int) bool {
		if item.Episodes[a].Season != item.Episodes[b].Season {
			return item.Episodes[a].Season < item.Episodes[b].Season
		}
		return item.Episodes[a].Episode < item.Episodes[b].Episode
	})
	return item, true
}

func libraryFile(key string, rec domain.CompletedRec, dir string) LibraryEpisode {
	path := rec.Path
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return LibraryEpisode{Key: key, Season: rec.Season, Episode: rec.Episode, Title: rec.Title,
		Path: path, Exists: path != "" && fsutil.IsNonEmptyFile(path), Bytes: rec.Bytes,
		Resolution: rec.Resolution, CompletedAt: rec.CompletedAt, Audio: rec.Audio,
		AudioFallback: rec.AudioFallback, RequestedQuality: rec.RequestedQuality, Quality: rec.Quality, AllAudio: rec.AllAudio}
}

func recoverLibraryAudio(ctx context.Context, ep *LibraryEpisode) {
	if ep.Exists && len(ep.Audio) == 0 {
		ep.Audio = mediainfo.AudioNames(ctx, ep.Path)
	}
}
