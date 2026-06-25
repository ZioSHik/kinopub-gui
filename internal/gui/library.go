package gui

import (
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
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

const stateFileName = ".kinopub-state.json"

// LibraryEpisode is one completed episode recorded in a state file.
type LibraryEpisode struct {
	Key         string    `json:"key"`
	Season      int       `json:"season"`
	Episode     int       `json:"episode"`
	Title       string    `json:"title"`
	Path        string    `json:"path"`
	Exists      bool      `json:"exists"`
	Bytes       int64     `json:"bytes"`
	Resolution  string    `json:"resolution,omitempty"`
	CompletedAt time.Time `json:"completedAt"`
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
	Type          string           `json:"type,omitempty"`   // kino.pub item type (movie, serial, …)
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

// DownloadedEpisode is one already-downloaded episode of a kino.pub item, used
// to mark which episodes the title card already has on disk.
type DownloadedEpisode struct {
	Key        string `json:"key"`
	Season     int    `json:"season"`
	Episode    int    `json:"episode"`
	Resolution string `json:"resolution,omitempty"`
	Exists     bool   `json:"exists"`
}

// DownloadedResponse lists the episodes of a kino.pub item already downloaded.
type DownloadedResponse struct {
	ID       string              `json:"id"`
	Dir      string              `json:"dir,omitempty"`
	Episodes []DownloadedEpisode `json:"episodes"`
}

// downloadedForItem scans the library roots for downloads belonging to the given
// kino.pub item id — matched on the recorded series id or, failing that, the id
// embedded in the saved InputURL — and returns the episodes already on disk.
func downloadedForItem(dirs []string, itemID string) DownloadedResponse {
	resp := DownloadedResponse{ID: itemID, Episodes: []DownloadedEpisode{}}
	if itemID == "" {
		return resp
	}
	for _, series := range scanLibrary(dirs).Series {
		if !seriesMatchesItem(series, itemID) {
			continue
		}
		if resp.Dir == "" {
			resp.Dir = series.Dir
		}
		for _, ep := range series.Episodes {
			resp.Episodes = append(resp.Episodes, DownloadedEpisode{
				Key:        ep.Key,
				Season:     ep.Season,
				Episode:    ep.Episode,
				Resolution: ep.Resolution,
				Exists:     ep.Exists,
			})
		}
	}
	return resp
}

// seriesMatchesItem reports whether a scanned series belongs to the kino.pub
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

	sort.Slice(resp.Series, func(a, b int) bool {
		return resp.Series[a].UpdatedAt.After(resp.Series[b].UpdatedAt)
	})
	return resp
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
	abs = filepath.Clean(abs)

	if _, err := os.Stat(filepath.Join(abs, stateFileName)); err != nil {
		return "", fmt.Errorf("not a kinopub download folder (no %s)", stateFileName)
	}

	for _, root := range roots {
		rabs, err := filepath.Abs(root)
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
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	var state domain.DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse state: %w", err)
	}
	rec, ok := state.Completed[key]
	if !ok {
		return fmt.Errorf("episode %q not found in this download", key)
	}

	// Resolve the media file relative to the series folder and confine the
	// deletion to it, so a tampered state file can't point us at an arbitrary
	// path. A file that's already gone is fine — the goal is that it's absent.
	fullPath := rec.Path
	if fullPath != "" && !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(abs, fullPath)
	}
	if fullPath != "" {
		clean := filepath.Clean(fullPath)
		rel, rerr := filepath.Rel(abs, clean)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("episode file is outside its series folder")
		}
		if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove episode file: %w", err)
		}
	}

	delete(state.Completed, key)

	// Last episode gone → remove the whole (now-empty) series folder.
	if len(state.Completed) == 0 {
		return os.RemoveAll(abs)
	}

	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return fsutil.AtomicWrite(stateFile, out, 0644)
}

// isSerialType reports whether a kino.pub item type denotes a series (serial,
// docuserial, tvshow) rather than a movie.
func isSerialType(t string) bool {
	t = strings.ToLower(t)
	return strings.Contains(t, "serial") || strings.Contains(t, "show")
}

// isMovieDownload classifies a scanned download as a movie or a series. New
// downloads carry the kino.pub item type; for older ones (recorded before the
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
		fullPath := rec.Path
		if fullPath != "" && !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(dir, fullPath)
		}
		exists := false
		if fullPath != "" {
			if _, statErr := os.Stat(fullPath); statErr == nil {
				exists = true
			}
		}
		item.Episodes = append(item.Episodes, LibraryEpisode{
			Key:         key,
			Season:      rec.Season,
			Episode:     rec.Episode,
			Title:       rec.Title,
			Path:        fullPath,
			Exists:      exists,
			Bytes:       rec.Bytes,
			Resolution:  rec.Resolution,
			CompletedAt: rec.CompletedAt,
		})
		item.TotalBytes += rec.Bytes
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
