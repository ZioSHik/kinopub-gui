// Package outputlayout derives filesystem paths for episode output and ensures
// the required directory structure exists.
package outputlayout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
)

// Layout implements domain.OutputLayout.
type Layout struct {
	ext string // file extension including the leading dot, e.g. ".mkv"
}

// New creates a Layout for the given container format.
// The container determines the output file extension (.mkv or .mp4).
func New(container domain.Container) *Layout {
	ext := ".mkv"
	if container == domain.ContainerMP4 {
		ext = ".mp4"
	}
	return &Layout{ext: ext}
}

// EpisodePath builds the full output path for an episode:
//
//	root/<sanitized series title>/Season <NN>/S<NN>E<NN>.<ext>
//
// The series directory name is derived from the series title via
// fsutil.SanitizeComponent, with a fallback based on the series ID.
// The season directory uses the episode's season number zero-padded to 2 digits.
// The filename uses S<NN>E<NN> format with the configured container extension.
func (l *Layout) EpisodePath(root string, series domain.Series, ep domain.Episode) (string, error) {

	// Derive season directory name.
	seasonDir := fmt.Sprintf("Season %02d", ep.Key.Season)

	// Derive episode filename.
	suffix := ""
	if ep.Quality != "" {
		suffix = "-" + fsutil.SanitizeComponent(ep.Quality, "optimal")
	}
	filename := fmt.Sprintf("S%02dE%02d%s%s", ep.Key.Season, ep.Key.Episode, suffix, l.ext)

	return filepath.Join(l.SeriesDir(root, series), seasonDir, filename), nil
}

// EnsureDirs creates all directories in the path up to and including the
// directory containing the file at path. It is idempotent: existing directories
// are not an error. Returns domain.ErrOutputDirUnwritable if the directories
// cannot be created.
func (l *Layout) EnsureDirs(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("%w: %s", domain.ErrOutputDirUnwritable, err.Error())
	}
	return nil
}

// SeriesDir reuses an old title-only directory only when its state proves it
// belongs to this item. Catalog IDs prevent namesakes from sharing files.
func (l *Layout) SeriesDir(root string, series domain.Series) string {
	title := fsutil.SanitizeComponent(series.Title, fmt.Sprintf("series_%s", series.ID))
	unique := filepath.Join(root, title+" [kp-"+fsutil.SanitizeComponent(string(series.ID), "unknown")+"]")
	if _, err := os.Stat(unique); err == nil {
		return unique
	}
	legacy := filepath.Join(root, title)
	data, err := os.ReadFile(filepath.Join(legacy, ".kinopub-state.json"))
	var state struct {
		Series domain.SeriesID `json:"series"`
	}
	if err == nil && json.Unmarshal(data, &state) == nil && state.Series == series.ID {
		return legacy
	}
	return unique
}
