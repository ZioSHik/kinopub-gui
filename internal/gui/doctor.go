package gui

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
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
	StateFile    string            `json:"stateFile"`
	SeriesID     string            `json:"seriesId,omitempty"`
	SeriesTitle  string            `json:"seriesTitle,omitempty"`
	TotalInState int               `json:"totalInState"`
	Healthy      int               `json:"healthy"`
	Fixed        bool              `json:"fixed"`
	HasIssues    bool              `json:"hasIssues"`
	Issues       []DoctorIssueView `json:"issues"`
	Logs         []LogEntry        `json:"logs,omitempty"`
}

func runDoctor(ctx context.Context, req DoctorRequest) (*DoctorReportView, error) {
	logger, capture := newCaptureLogger(domain.VerbosityNormal)

	// API-only build: the doctor verifies files against the state file
	// (presence + recorded size) and cleans orphan temp files. Source-duration
	// re-resolution (cookie/RSS) was removed, so no resolvers are wired.
	deps := doctor.Deps{Logger: logger}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// The state file lives inside each series' own directory, not at the output
	// root. Discover every series directory under OutputDir and check each, so
	// pointing the doctor at the top-level downloads folder works. If none are
	// found, fall back to OutputDir itself (preserving the original
	// "nothing to check" message for a truly empty folder).
	dirs := findStateDirs(req.OutputDir)
	if len(dirs) == 0 {
		dirs = []string{req.OutputDir}
	}

	view := &DoctorReportView{Fixed: req.Fix}
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
	// Only surface an error if no series could be checked at all.
	if view.StateFile == "" && firstErr != nil {
		return nil, firstErr
	}
	view.Logs = capture.entries
	return view, nil
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
