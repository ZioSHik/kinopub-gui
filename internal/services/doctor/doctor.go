// Package doctor implements the "doctor" subcommand that verifies downloaded
// files against the state file and repairs inconsistencies.
//
// It detects:
//   - Files recorded in state but missing on disk
//   - Files whose on-disk size doesn't match the recorded bytes
//   - State entries with empty path or zero bytes (incomplete records)
//   - Orphan .tmp files left from interrupted downloads
//
// Repair actions:
//   - Remove state entries for broken files so they get re-downloaded
//   - Delete truncated/corrupt files from disk
//   - Delete orphan .tmp files
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
)

// Issue describes a single problem found during verification.
type Issue struct {
	Key         string // "S1E16" style key
	Season      int
	Episode     int
	Kind        IssueKind
	Detail      string
	StatePath   string // path recorded in state
	StateBytes  int64  // bytes recorded in state
	ActualBytes int64  // actual file size on disk (-1 if missing)
	Version     int    // index inside CompletedRec.Files()
}

// IssueKind classifies the type of problem.
type IssueKind int

const (
	IssueMissing      IssueKind = iota // file not found on disk
	IssueTruncated                     // file smaller than recorded
	IssueSizeMismatch                  // file size differs (larger)
	IssueNoPath                        // state entry has no path
	IssueOrphanTmp                     // orphan .tmp file
)

// String returns a human-readable label for the issue kind.
func (k IssueKind) String() string {
	switch k {
	case IssueMissing:
		return "MISSING"
	case IssueTruncated:
		return "TRUNCATED"
	case IssueSizeMismatch:
		return "SIZE_MISMATCH"
	case IssueNoPath:
		return "NO_PATH"
	case IssueOrphanTmp:
		return "ORPHAN_TMP"
	default:
		return "UNKNOWN"
	}
}

// Report is the result of a doctor check.
type Report struct {
	StateFile    string
	SeriesID     string
	SeriesTitle  string
	TotalInState int
	Healthy      int
	Issues       []Issue
	OrphanTmps   []string
}

// HasIssues reports whether any problems were found.
func (r *Report) HasIssues() bool {
	return len(r.Issues) > 0 || len(r.OrphanTmps) > 0
}

// Deps holds the injectable dependencies for the doctor.
type Deps struct {
	Logger domain.Logger
}

// Options configures the doctor behavior.
type Options struct {
	// OutputDir is the directory where downloads are stored and the state file lives.
	OutputDir string
	// Fix when true, automatically repairs the state file.
	Fix bool
	// CleanTmp when true, deletes orphan .tmp files.
	CleanTmp bool
}

const stateFileName = ".kinopub-state.json"

// Run performs the doctor check and optionally repairs issues.
func Run(ctx context.Context, deps Deps, opts Options) (*Report, error) {
	log := deps.Logger.Component("doctor")
	opts.OutputDir = fsutil.CanonicalPath(opts.OutputDir)

	stateFilePath := filepath.Join(opts.OutputDir, stateFileName)

	// 1. Load and parse state file.
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("state file not found at %s — nothing to check", stateFilePath)
		}
		return nil, fmt.Errorf("cannot read state file: %w", err)
	}

	var state domain.DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Error("state file is corrupt JSON",
			domain.F("path", stateFilePath),
			domain.F("error", err.Error()),
		)
		if opts.Fix {
			backupPath := stateFilePath + ".corrupt." + time.Now().Format("20060102-150405.000000000")
			if copyErr := copyFile(stateFilePath, backupPath); copyErr == nil {
				log.Info("backed up corrupt state file", domain.F("backup", backupPath))
			}
			emptyState := domain.DownloadState{
				Series:    "",
				Completed: make(map[string]domain.CompletedRec),
			}
			if writeErr := writeState(stateFilePath, emptyState); writeErr != nil {
				return nil, fmt.Errorf("failed to write repaired state: %w", writeErr)
			}
			log.Info("replaced corrupt state with empty state")
			return &Report{
				StateFile: stateFilePath,
				Issues: []Issue{{
					Kind:   IssueMissing,
					Detail: "state file was corrupt JSON and has been reset",
				}},
			}, nil
		}
		return nil, fmt.Errorf("state file is corrupt JSON: %w (use --fix to reset)", err)
	}

	if state.Completed == nil {
		state.Completed = make(map[string]domain.CompletedRec)
	}

	report := &Report{
		StateFile:    stateFilePath,
		SeriesID:     string(state.Series),
		TotalInState: len(state.Completed),
	}
	if state.Metadata != nil {
		report.SeriesTitle = state.Metadata.Title
	}

	// 2. Check each completed entry against the filesystem (presence + size).
	for key, rec := range state.Completed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		healthy := true
		for version, file := range rec.Files() {
			if issue := checkEntry(key, file, opts.OutputDir); issue != nil {
				issue.Version = version
				report.Issues = append(report.Issues, *issue)
				healthy = false
			}
		}
		if healthy {
			report.Healthy++
		}
	}

	// Sort issues by key for stable output.
	sort.Slice(report.Issues, func(i, j int) bool {
		return report.Issues[i].Key < report.Issues[j].Key
	})

	// 3. Scan for orphan .tmp files.
	orphans := findOrphanTmps(opts.OutputDir)
	report.OrphanTmps = orphans
	for _, tmp := range orphans {
		report.Issues = append(report.Issues, Issue{
			Kind:      IssueOrphanTmp,
			Detail:    tmp,
			StatePath: tmp,
		})
	}

	// 4. Apply fixes if requested.
	if (opts.Fix || opts.CleanTmp) && len(report.Issues) > 0 {
		fixed, err := applyFixes(stateFilePath, &state, report, opts, log)
		if err != nil {
			return report, err
		}
		log.Info("fixes applied", domain.F("entries_removed", fixed))
	}

	return report, nil
}

// checkEntry verifies a single completed record against the filesystem (size only).
func checkEntry(key string, rec domain.CompletedRec, outputDir string) *Issue {
	base := Issue{
		Key:        key,
		Season:     rec.Season,
		Episode:    rec.Episode,
		StatePath:  rec.Path,
		StateBytes: rec.Bytes,
	}

	if rec.Path == "" {
		if rec.Bytes == 0 {
			base.Kind = IssueNoPath
			base.Detail = "state entry has no file path and zero bytes (incomplete record)"
			base.ActualBytes = -1
			return &base
		}
		base.Kind = IssueNoPath
		base.Detail = fmt.Sprintf("state entry has no file path but records %d bytes", rec.Bytes)
		base.ActualBytes = -1
		return &base
	}

	fullPath := rec.Path
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(outputDir, fullPath)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			base.Kind = IssueMissing
			base.Detail = fmt.Sprintf("file not found: %s", fullPath)
			base.ActualBytes = -1
			return &base
		}
		base.Kind = IssueMissing
		base.Detail = fmt.Sprintf("cannot stat file: %v", err)
		base.ActualBytes = -1
		return &base
	}

	if !info.Mode().IsRegular() {
		base.Kind = IssueNoPath
		base.Detail = "recorded path is not a regular media file"
		return &base
	}
	actualSize := info.Size()
	if actualSize == 0 {
		base.Kind = IssueTruncated
		base.Detail = "media file is empty"
		return &base
	}
	base.ActualBytes = actualSize

	if rec.Bytes > 0 && actualSize < rec.Bytes {
		base.Kind = IssueTruncated
		base.Detail = fmt.Sprintf("file is truncated: %d bytes on disk vs %d recorded (%.1f%%)",
			actualSize, rec.Bytes, float64(actualSize)/float64(rec.Bytes)*100)
		return &base
	}

	if rec.Bytes > 0 && actualSize != rec.Bytes {
		base.Kind = IssueSizeMismatch
		base.Detail = fmt.Sprintf("size mismatch: %d bytes on disk vs %d recorded",
			actualSize, rec.Bytes)
		return &base
	}

	return nil
}

// findOrphanTmps walks the output directory looking for orphan .tmp files and
// .hls-tmp segment directories left by interrupted HLS downloads.
func findOrphanTmps(outputDir string) []string {
	var orphans []string
	_ = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Orphan HLS segment directory ("<episode>.ts.hls-tmp") left by an
			// interrupted download. These do NOT start with a dot, so check the
			// suffix before the hidden-directory skip below.
			if strings.HasSuffix(info.Name(), ".hls-tmp") {
				// Derive the expected final media path by stripping the
				// ".ts.hls-tmp" / ".hls-tmp" suffix, then check whether a
				// completed media file exists next to it.
				base := strings.TrimSuffix(path, ".hls-tmp")
				base = strings.TrimSuffix(base, ".ts")
				// The final file could be .mkv, .mp4, etc.; we look for any
				// sibling file that shares the same base name (without ext).
				dir := filepath.Dir(path)
				stem := filepath.Base(base)
				entries, readErr := os.ReadDir(dir)
				hasMedia := false
				if readErr == nil {
					for _, entry := range entries {
						if entry.IsDir() {
							continue
						}
						n := entry.Name()
						if strings.HasPrefix(n, stem) && n != filepath.Base(path) {
							hasMedia = true
							break
						}
					}
				}
				if !hasMedia {
					orphans = append(orphans, path)
				}
				return filepath.SkipDir
			}
			// Skip hidden directories (e.g. .git).
			if strings.HasPrefix(info.Name(), ".") && path != outputDir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".tmp") {
			finalPath := strings.TrimSuffix(path, ".tmp")
			if _, err := os.Stat(finalPath); os.IsNotExist(err) {
				orphans = append(orphans, path)
			}
		}
		return nil
	})
	return orphans
}

// applyFixes modifies the state file to remove broken entries.
func applyFixes(stateFilePath string, state *domain.DownloadState, report *Report, opts Options, log domain.Logger) (int, error) {
	bad := make(map[string]map[int]bool)
	var brokenFiles, temporaryFiles []string
	for _, issue := range report.Issues {
		switch issue.Kind {
		case IssueMissing, IssueTruncated, IssueNoPath:
			if !opts.Fix || issue.Key == "" {
				continue
			}
			if bad[issue.Key] == nil {
				bad[issue.Key] = make(map[int]bool)
			}
			bad[issue.Key][issue.Version] = true
			if issue.Kind == IssueTruncated && issue.StatePath != "" {
				path, err := repairPath(opts.OutputDir, issue.StatePath)
				if err != nil {
					return 0, err
				}
				brokenFiles = append(brokenFiles, path)
			}
		case IssueOrphanTmp:
			if opts.CleanTmp {
				path, err := repairPath(opts.OutputDir, issue.Detail)
				if err != nil {
					return 0, err
				}
				temporaryFiles = append(temporaryFiles, path)
			}
		}
	}
	removed := 0
	for key, versions := range bad {
		rec, ok := state.Completed[key]
		if !ok {
			continue
		}
		var kept []domain.CompletedRec
		for index, file := range rec.Files() {
			if versions[index] {
				removed++
				continue
			}
			kept = append(kept, file)
		}
		if len(kept) == 0 {
			delete(state.Completed, key)
		} else {
			primary := kept[0]
			primary.Versions = kept[1:]
			state.Completed[key] = primary
		}
	}
	if removed > 0 {
		if err := writeState(stateFilePath, *state); err != nil {
			return 0, fmt.Errorf("write repaired state: %w", err)
		}
		log.Info("state file repaired", domain.F("records_removed", removed))
	}
	for _, path := range append(brokenFiles, temporaryFiles...) {
		var err error
		if strings.HasSuffix(path, ".hls-tmp") {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("remove broken/temporary file %q: %w", path, err)
		}
	}
	return removed, nil
}

// A state record must never grant cleanup permission outside this library root.
func repairPath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	root, path = fsutil.CanonicalPath(root), fsutil.CanonicalPath(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to repair a file outside the checked folder: %q", path)
	}
	return path, nil
}

// writeState serializes and atomically writes the state to disk.
func writeState(path string, state domain.DownloadState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return fsutil.AtomicWrite(path, data, 0600)
}

// copyFile copies src to dst for backup purposes.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(dst, data, 0600)
}
