package gui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
	"github.com/ZioSHik/kinopub-gui/internal/services/doctor"
)

// DoctorRequest is the body of POST /api/doctor.
type DoctorRequest struct {
	OutputDir string `json:"outputDir"`
	Fix       bool   `json:"fix"`
	CleanTmp  bool   `json:"cleanTmp"`
}

// DoctorIssueView is a serialized doctor.Issue.
type DoctorIssueView struct {
	Key         string `json:"key,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Kind        string `json:"kind"`
	Detail      string `json:"detail"`
	StatePath   string `json:"statePath,omitempty"`
	StateBytes  int64  `json:"stateBytes,omitempty"`
	ActualBytes int64  `json:"actualBytes,omitempty"`
}

// DoctorReportView is the serialized doctor.Report plus captured logs.
type DoctorReportView struct {
	StateFile    string `json:"stateFile"`
	SeriesID     string `json:"seriesId,omitempty"`
	SeriesTitle  string `json:"seriesTitle,omitempty"`
	TotalInState int    `json:"totalInState"`
	Healthy      int    `json:"healthy"`
	Fixed        bool   `json:"fixed"`
	HasIssues    bool   `json:"hasIssues"`
	// RepairBlocked reports that repair/cleanup was requested but refused because
	// unfinished downloads are writing into the same folder. The check itself
	// still ran, so the user sees what's wrong — just nothing was deleted.
	RepairBlocked bool              `json:"repairBlocked,omitempty"`
	Issues        []DoctorIssueView `json:"issues"`
	Logs          []LogEntry        `json:"logs,omitempty"`
}

// runDoctor checks the downloads under req.OutputDir. liveRoots are the output
// folders of downloads that are not finished (see JobManager.liveOutputPaths);
// when the checked folder overlaps one of them, the destructive half of the
// doctor is disabled — see the guard below.
func runDoctor(ctx context.Context, req DoctorRequest, liveRoots []string) (*DoctorReportView, error) {
	logger, capture := newCaptureLogger(domain.VerbosityNormal)

	// API-only build: the doctor verifies files against the state file
	// (presence + recorded size) and cleans orphan temp files. Source-duration
	// re-resolution (cookie/RSS) was removed, so no resolvers are wired.
	deps := doctor.Deps{Logger: logger}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// A download that hasn't finished lives in exactly the files the doctor would
	// delete: the engine writes each episode to "<episode>.tmp" (progressive) or
	// collects segments in "<episode>.ts.hls-tmp" (HLS), and a paused or failed
	// job resumes from them. Neither has a finished media file next to it yet,
	// which is precisely how findOrphanTmps recognizes an orphan — so cleaning
	// "leftovers" mid-queue would wipe out every megabyte already fetched.
	// Repair has the same problem from the other side: it rewrites the state file
	// that the running engine is updating as episodes complete.
	// So when the folder under the microscope overlaps a live download's output
	// folder, we still report, but we don't touch anything.
	blocked := false
	for _, root := range liveRoots {
		if pathsOverlap(req.OutputDir, root) {
			blocked = true
			break
		}
	}
	if blocked && (req.Fix || req.CleanTmp) {
		req.Fix = false
		req.CleanTmp = false
		logger.Component("doctor").Warn("repair skipped: downloads in progress are using this folder",
			domain.F("dir", req.OutputDir),
		)
	} else {
		blocked = false // nothing destructive was asked for; don't cry wolf
	}

	// The state file lives inside each series' own directory, not at the output
	// root. Discover every series directory under OutputDir and check each, so
	// pointing the doctor at the top-level downloads folder works. If none are
	// found, fall back to OutputDir itself (preserving the original
	// "nothing to check" message for a truly empty folder).
	dirs := findStateDirs(req.OutputDir)
	if len(dirs) == 0 {
		dirs = []string{req.OutputDir}
	}

	view := &DoctorReportView{Fixed: req.Fix, RepairBlocked: blocked}
	var firstErr error
	for _, dir := range dirs {
		report, err := doctor.Run(ctx, deps, doctor.Options{
			OutputDir: dir,
			Fix:       req.Fix,
			CleanTmp:  req.CleanTmp,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if view.StateFile == "" {
			view.StateFile = report.StateFile
			view.SeriesID = report.SeriesID
			view.SeriesTitle = report.SeriesTitle
		} else if report.SeriesTitle != "" {
			view.SeriesTitle = view.SeriesTitle + ", " + report.SeriesTitle
		}
		view.TotalInState += report.TotalInState
		view.Healthy += report.Healthy
		if report.HasIssues() {
			view.HasIssues = true
		}
		for _, iss := range report.Issues {
			view.Issues = append(view.Issues, DoctorIssueView{
				Key:         iss.Key,
				Season:      iss.Season,
				Episode:     iss.Episode,
				Kind:        iss.Kind.String(),
				Detail:      iss.Detail,
				StatePath:   iss.StatePath,
				StateBytes:  iss.StateBytes,
				ActualBytes: iss.ActualBytes,
			})
		}
	}
	// Do not present a partially failed check or repair as a successful run.
	if firstErr != nil {
		return nil, firstErr
	}
	view.Logs = capture.entries
	return view, nil
}

// pathsOverlap reports whether two directories are the same or one contains the
// other. Both are resolved to absolute, cleaned paths first; on case-insensitive
// filesystems (macOS, Windows) the comparison ignores case, so a differently
// typed path can't slip past a guard that relies on this.
func pathsOverlap(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, bb := fsutil.CanonicalPath(a), fsutil.CanonicalPath(b)
	aa, bb = filepath.Clean(aa), filepath.Clean(bb)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		aa, bb = strings.ToLower(aa), strings.ToLower(bb)
	}
	return aa == bb || isUnder(aa, bb) || isUnder(bb, aa)
}

// isUnder reports whether child sits inside parent (both cleaned absolute paths).
func isUnder(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// findStateDirs returns the directories under root that contain a kinopub state
// file (the series directories the doctor must be pointed at).
func findStateDirs(root string) []string {
	if root == "" {
		return nil
	}
	var dirs []string
	seen := make(map[string]bool)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == stateFileName {
			dir := filepath.Dir(path)
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs
}
