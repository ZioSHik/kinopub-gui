package gui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

// LeftoverView reports the partial download data a job is still holding on
// disk, so the UI can say what removing its card would throw away.
type LeftoverView struct {
	Bytes int64 `json:"bytes"`
	Items int   `json:"items"`
	// Conflict marks the data as shared with another job that can still resume
	// it (same series, same episodes). Removing the card then leaves the files
	// alone — they are that job's resume data, not litter.
	Conflict bool `json:"conflict"`
}

// episodeStem is the filename prefix the engine derives every path for an
// episode from: "S01E02" for "S01E02.mkv", "S01E02.mkv.ts.hls-tmp", etc.
func episodeStem(season, episode int) string {
	return fmt.Sprintf("S%02dE%02d", season, episode)
}

// jobTempPaths lists the partial-download files a job left on disk: the
// "<episode>.ts.hls-tmp" segment directories (HLS) and "<episode>.tmp" part
// files (progressive) that the engine deliberately keeps so a paused or
// canceled episode can resume later.
//
// The search is scoped twice over — to the job's own series folder, and to the
// stems of its own episodes — so a job that shares the downloads root (all of
// them do) or even the same series folder never has another job's resume data
// swept up with its own.
func jobTempPaths(seriesDir string, stems map[string]bool) []string {
	if seriesDir == "" || len(stems) == 0 {
		return nil
	}
	var paths []string
	_ = filepath.WalkDir(seriesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable or missing: nothing to report
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".hls-tmp") && !strings.HasSuffix(name, ".tmp") {
			return nil
		}
		// "S01E02.mkv.ts.hls-tmp" → "S01E02". A temp name always carries the
		// episode stem ahead of the first dot, so this identifies the owner
		// without depending on the configured container extension.
		stem, _, _ := strings.Cut(name, ".")
		stem, _, _ = strings.Cut(stem, "-")
		if !stems[stem] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		paths = append(paths, path)
		if d.IsDir() {
			return fs.SkipDir // counted whole; no need to walk the segments
		}
		return nil
	})
	return paths
}

// pathSize sums the bytes at path, walking it when it is a directory (an
// .hls-tmp folder holds one file per downloaded segment).
func pathSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// jobScope is everything needed to locate a job's partial files: where they
// live and which episodes they may belong to.
type jobScope struct {
	dir   string
	stems map[string]bool
}

// scopeOf resolves where a job's partial files live and which episodes they may
// belong to. Caller must not hold j.mu.
func scopeOf(j *Job) jobScope {
	j.mu.Lock()
	dir, outputPath, url := j.seriesDir, j.outputPath, j.url
	stems := make(map[string]bool, len(j.episodes))
	for _, ev := range j.episodes {
		// A completed episode has no partial data left, and its finished file
		// must never be mistaken for litter.
		if ev.State != epCompleted {
			stems[episodeStem(ev.Season, ev.Episode)] = true
		}
	}
	j.mu.Unlock()

	if dir == "" {
		// A card persisted before the engine started reporting its folder. The
		// display title can't stand in for it (a job started from a search result
		// carries a short title while the folder holds the full one), so go by the
		// item id recorded in the state file the download left behind. The result
		// is cached back onto the job: leftovers/purgeConflict/purgeTemp all call
		// scopeOf, and each walk over a large (or network-mounted) library is paid
		// once per job instead of once per call.
		if dir = seriesDirByItem(outputPath, url); dir != "" {
			j.mu.Lock()
			if j.seriesDir == "" {
				j.seriesDir = dir
			}
			j.mu.Unlock()
		}
	}
	return jobScope{dir: dir, stems: stems}
}

// seriesDirByItem finds the folder a job downloaded into by matching the
// kino.watch item it points at against the state files under the output root.
// Returns "" when nothing matches — a run that never resolved may not have
// written a state file at all.
//
// It walks the state files directly instead of going through scanLibrary: the
// library scan deliberately skips metadata-only state files (zero completed
// episodes — not a download the user has), but for locating a job's partial
// data such a file is exactly the trace an old-version run that paused before
// its first completion left next to its multi-gigabyte .hls-tmp segments.
func seriesDirByItem(outputPath, url string) string {
	if outputPath == "" || url == "" {
		return ""
	}
	itemID := kinopubapi.ItemIDFromURL(url)
	if itemID == "" {
		return ""
	}
	var found string
	_ = filepath.WalkDir(outputPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != outputPath && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != stateFileName {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var state domain.DownloadState
		if json.Unmarshal(data, &state) != nil {
			return nil
		}
		matches := string(state.Series) == itemID
		if !matches && state.Metadata != nil && state.Metadata.InputURL != "" {
			matches = kinopubapi.ItemIDFromURL(state.Metadata.InputURL) == itemID
		}
		if matches {
			found = filepath.Dir(path)
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// leftovers reports what removing this job's card would leave behind (or take
// with it). Returns false if the job is unknown.
func (m *JobManager) leftovers(id string) (LeftoverView, bool) {
	j, ok := m.get(id)
	if !ok {
		return LeftoverView{}, false
	}
	sc := scopeOf(j)
	paths := jobTempPaths(sc.dir, sc.stems)
	if len(paths) == 0 {
		return LeftoverView{}, true
	}
	view := LeftoverView{Items: len(paths), Conflict: m.purgeConflict(id, sc.dir, sc.stems)}
	for _, p := range paths {
		view.Bytes += pathSize(p)
	}
	return view, true
}

// purgeConflict reports whether another job that can still resume owns any of
// the same partial files. Every job shares one downloads root, so the check is
// on what would actually be deleted — the series folder and the episode
// numbers — rather than on the root path.
func (m *JobManager) purgeConflict(exceptID, dir string, stems map[string]bool) bool {
	if dir == "" {
		return false
	}
	m.mu.RLock()
	others := make([]*Job, 0, len(m.jobs))
	for id, j := range m.jobs {
		if id != exceptID {
			others = append(others, j)
		}
	}
	m.mu.RUnlock()

	for _, j := range others {
		j.mu.Lock()
		// A failed job is not done: it keeps a Retry button, and these very files
		// are what the retry would resume from.
		done := isFinishedStatus(j.status)
		j.mu.Unlock()
		if done {
			continue
		}
		sc := scopeOf(j)
		if sc.dir != dir {
			continue
		}
		for stem := range sc.stems {
			if stems[stem] {
				return true
			}
		}
	}
	return false
}

// purgeTemp deletes the job's partial download data. It returns the bytes it
// freed and how many items could NOT be deleted (a file held open by an
// antivirus/indexer, a permissions change) so the caller can surface the
// failure instead of reporting a clean purge over data still on disk. It is a
// no-op when another job can still resume the same files.
//
// It takes the job itself rather than an id because remove() calls it after
// dropping the card from the queue: by then nothing can resume the job (the
// removed flag refuses stale resumes, and a lookup by id no longer finds it),
// so no engine can be writing the files underneath.
func (m *JobManager) purgeTemp(j *Job) (freed int64, failed int) {
	sc := scopeOf(j)
	if m.purgeConflict(j.id, sc.dir, sc.stems) {
		return 0, 0
	}
	for _, p := range jobTempPaths(sc.dir, sc.stems) {
		size := pathSize(p)
		if err := os.RemoveAll(p); err != nil {
			failed++
			continue
		}
		freed += size
	}
	return freed, failed
}
