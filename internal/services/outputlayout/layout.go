// Package outputlayout derives filesystem paths for episode output and ensures
// the required directory structure exists.
package outputlayout

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/fsutil"
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
	// Derive series directory name.
	fallback := fmt.Sprintf("series_%s", string(series.ID))
	seriesDir := fsutil.SanitizeComponent(series.Title, fallback)

	// Derive season directory name.
	seasonDir := fmt.Sprintf("Season %02d", ep.Key.Season)

	// Derive episode filename.
	filename := fmt.Sprintf("S%02dE%02d%s", ep.Key.Season, ep.Key.Episode, l.ext)

	return filepath.Join(root, seriesDir, seasonDir, filename), nil
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
