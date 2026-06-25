package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// nopLogger is a no-op domain.Logger for tests.
type nopLogger struct{}

func (nopLogger) Debug(_ string, _ ...domain.Field)      {}
func (nopLogger) Info(_ string, _ ...domain.Field)       {}
func (nopLogger) Warn(_ string, _ ...domain.Field)       {}
func (nopLogger) Error(_ string, _ ...domain.Field)      {}
func (l nopLogger) With(_ ...domain.Field) domain.Logger { return l }
func (l nopLogger) Component(_ string) domain.Logger     { return l }

func testDeps() Deps { return Deps{Logger: nopLogger{}} }

// writeStateFile writes a DownloadState as JSON to outputDir's state file.
func writeStateFile(t *testing.T, outputDir string, state domain.DownloadState) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, stateFileName), data, 0644); err != nil {
		t.Fatalf("write state file: %v", err)
	}
}

// writeRawState writes raw bytes as the state file (for corrupt-JSON cases).
func writeRawState(t *testing.T, outputDir string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(outputDir, stateFileName), raw, 0644); err != nil {
		t.Fatalf("write raw state file: %v", err)
	}
}

// mkfile creates a file with the given byte length under dir at rel path.
func mkfile(t *testing.T, dir, rel string, size int) string {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, make([]byte, size), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return full
}

func readState(t *testing.T, outputDir string) domain.DownloadState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outputDir, stateFileName))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var s domain.DownloadState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	return s
}

// ---------------------------------------------------------------------------
// IssueKind.String
// ---------------------------------------------------------------------------

func TestIssueKindString(t *testing.T) {
	cases := []struct {
		k    IssueKind
		want string
	}{
		{IssueMissing, "MISSING"},
		{IssueTruncated, "TRUNCATED"},
		{IssueSizeMismatch, "SIZE_MISMATCH"},
		{IssueNoPath, "NO_PATH"},
		{IssueOrphanTmp, "ORPHAN_TMP"},
		{IssueKind(99), "UNKNOWN"},
		{IssueKind(-1), "UNKNOWN"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("IssueKind(%d).String() = %q, want %q", int(c.k), got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Report.HasIssues
// ---------------------------------------------------------------------------

func TestReportHasIssues(t *testing.T) {
	cases := []struct {
		name string
		r    Report
		want bool
	}{
		{"empty", Report{}, false},
		{"with issue", Report{Issues: []Issue{{Kind: IssueMissing}}}, true},
		{"with orphan", Report{OrphanTmps: []string{"x.tmp"}}, true},
		{"both", Report{Issues: []Issue{{}}, OrphanTmps: []string{"x"}}, true},
	}
	for _, c := range cases {
		if got := c.r.HasIssues(); got != c.want {
			t.Errorf("%s: HasIssues() = %v, want %v", c.name, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// checkEntry: pure verification logic
// ---------------------------------------------------------------------------

func TestCheckEntry_Healthy(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "ep1.mkv", 100)
	rec := domain.CompletedRec{Season: 1, Episode: 1, Path: "ep1.mkv", Bytes: 100}
	if issue := checkEntry("S1E1", rec, dir); issue != nil {
		t.Fatalf("expected healthy (nil issue), got %+v", issue)
	}
}

func TestCheckEntry_HealthyAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	full := mkfile(t, dir, "ep1.mkv", 50)
	rec := domain.CompletedRec{Path: full, Bytes: 50}
	if issue := checkEntry("S1E1", rec, dir); issue != nil {
		t.Fatalf("expected healthy for abs path, got %+v", issue)
	}
}

func TestCheckEntry_HealthyZeroRecordedBytes(t *testing.T) {
	// rec.Bytes == 0 with a path present: file exists, no size guards apply.
	dir := t.TempDir()
	mkfile(t, dir, "ep1.mkv", 0)
	rec := domain.CompletedRec{Path: "ep1.mkv", Bytes: 0}
	if issue := checkEntry("S1E1", rec, dir); issue != nil {
		t.Fatalf("expected healthy when recorded bytes 0 and file present, got %+v", issue)
	}
}

func TestCheckEntry_NoPathZeroBytes(t *testing.T) {
	rec := domain.CompletedRec{Path: "", Bytes: 0, Season: 2, Episode: 3}
	issue := checkEntry("S2E3", rec, t.TempDir())
	if issue == nil {
		t.Fatal("expected issue, got nil")
	}
	if issue.Kind != IssueNoPath {
		t.Errorf("Kind = %v, want IssueNoPath", issue.Kind)
	}
	if issue.ActualBytes != -1 {
		t.Errorf("ActualBytes = %d, want -1", issue.ActualBytes)
	}
	if !strings.Contains(issue.Detail, "zero bytes") {
		t.Errorf("Detail = %q, want mention of zero bytes", issue.Detail)
	}
	if issue.Key != "S2E3" || issue.Season != 2 || issue.Episode != 3 {
		t.Errorf("base fields not propagated: %+v", issue)
	}
}

func TestCheckEntry_NoPathWithBytes(t *testing.T) {
	rec := domain.CompletedRec{Path: "", Bytes: 1234}
	issue := checkEntry("S1E1", rec, t.TempDir())
	if issue == nil || issue.Kind != IssueNoPath {
		t.Fatalf("expected IssueNoPath, got %+v", issue)
	}
	if !strings.Contains(issue.Detail, "1234 bytes") {
		t.Errorf("Detail = %q, want byte count", issue.Detail)
	}
	if issue.ActualBytes != -1 {
		t.Errorf("ActualBytes = %d, want -1", issue.ActualBytes)
	}
}

func TestCheckEntry_Missing(t *testing.T) {
	dir := t.TempDir()
	rec := domain.CompletedRec{Path: "ghost.mkv", Bytes: 100}
	issue := checkEntry("S1E1", rec, dir)
	if issue == nil || issue.Kind != IssueMissing {
		t.Fatalf("expected IssueMissing, got %+v", issue)
	}
	if issue.ActualBytes != -1 {
		t.Errorf("ActualBytes = %d, want -1", issue.ActualBytes)
	}
	if !strings.Contains(issue.Detail, "file not found") {
		t.Errorf("Detail = %q", issue.Detail)
	}
}

func TestCheckEntry_Truncated(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "ep1.mkv", 40)
	rec := domain.CompletedRec{Path: "ep1.mkv", Bytes: 100}
	issue := checkEntry("S1E1", rec, dir)
	if issue == nil || issue.Kind != IssueTruncated {
		t.Fatalf("expected IssueTruncated, got %+v", issue)
	}
	if issue.ActualBytes != 40 {
		t.Errorf("ActualBytes = %d, want 40", issue.ActualBytes)
	}
	if !strings.Contains(issue.Detail, "40.0%") {
		t.Errorf("Detail = %q, want 40.0%% progress", issue.Detail)
	}
}

func TestCheckEntry_SizeMismatchLarger(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "ep1.mkv", 150)
	rec := domain.CompletedRec{Path: "ep1.mkv", Bytes: 100}
	issue := checkEntry("S1E1", rec, dir)
	if issue == nil || issue.Kind != IssueSizeMismatch {
		t.Fatalf("expected IssueSizeMismatch, got %+v", issue)
	}
	if issue.ActualBytes != 150 {
		t.Errorf("ActualBytes = %d, want 150", issue.ActualBytes)
	}
}

func TestCheckEntry_StatErrorNotNotExist(t *testing.T) {
	// A path whose parent component is a regular file yields ENOTDIR (not
	// IsNotExist), exercising the "cannot stat file" branch.
	dir := t.TempDir()
	mkfile(t, dir, "afile", 10)
	rec := domain.CompletedRec{Path: filepath.Join("afile", "child.mkv"), Bytes: 100}
	issue := checkEntry("S1E1", rec, dir)
	if issue == nil || issue.Kind != IssueMissing {
		t.Fatalf("expected IssueMissing for stat error, got %+v", issue)
	}
	if issue.ActualBytes != -1 {
		t.Errorf("ActualBytes = %d, want -1", issue.ActualBytes)
	}
	if !strings.Contains(issue.Detail, "cannot stat file") {
		t.Errorf("Detail = %q, want 'cannot stat file'", issue.Detail)
	}
}

func TestCheckEntry_ExactSizeHealthy(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "ep1.mkv", 100)
	rec := domain.CompletedRec{Path: "ep1.mkv", Bytes: 100}
	if issue := checkEntry("S1E1", rec, dir); issue != nil {
		t.Fatalf("expected healthy on exact match, got %+v", issue)
	}
}

// ---------------------------------------------------------------------------
// findOrphanTmps
// ---------------------------------------------------------------------------

func TestFindOrphanTmps_OrphanAndCompanion(t *testing.T) {
	dir := t.TempDir()
	// orphan: a.mkv.tmp with NO a.mkv
	mkfile(t, dir, "a.mkv.tmp", 10)
	// not orphan: b.mkv.tmp WITH b.mkv present
	mkfile(t, dir, "b.mkv.tmp", 10)
	mkfile(t, dir, "b.mkv", 100)

	orphans := findOrphanTmps(dir)
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d: %v", len(orphans), orphans)
	}
	if filepath.Base(orphans[0]) != "a.mkv.tmp" {
		t.Errorf("orphan = %q, want a.mkv.tmp", orphans[0])
	}
}

func TestFindOrphanTmps_None(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.mkv", 100)
	if orphans := findOrphanTmps(dir); len(orphans) != 0 {
		t.Fatalf("expected no orphans, got %v", orphans)
	}
}

func TestFindOrphanTmps_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	// A .tmp file buried inside a hidden directory must be skipped.
	mkfile(t, dir, ".git/objects/foo.mkv.tmp", 10)
	if orphans := findOrphanTmps(dir); len(orphans) != 0 {
		t.Fatalf("expected hidden-dir .tmp to be skipped, got %v", orphans)
	}
}

func TestFindOrphanTmps_HlsTmpOrphanDir(t *testing.T) {
	dir := t.TempDir()
	// Orphan HLS segment dir with no sibling media file.
	if err := os.MkdirAll(filepath.Join(dir, "ep1.ts.hls-tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	orphans := findOrphanTmps(dir)
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan hls-tmp dir, got %v", orphans)
	}
	if !strings.HasSuffix(orphans[0], ".hls-tmp") {
		t.Errorf("orphan = %q, want .hls-tmp suffix", orphans[0])
	}
}

func TestFindOrphanTmps_HlsTmpWithMediaNotOrphan(t *testing.T) {
	dir := t.TempDir()
	// Completed media file sits next to the .hls-tmp dir → not an orphan.
	if err := os.MkdirAll(filepath.Join(dir, "ep1.ts.hls-tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	mkfile(t, dir, "ep1.mkv", 100)
	orphans := findOrphanTmps(dir)
	if len(orphans) != 0 {
		t.Fatalf("expected no orphan when media present, got %v", orphans)
	}
}

func TestFindOrphanTmps_NonexistentDir(t *testing.T) {
	// Walking a missing dir must not panic and returns no orphans.
	if orphans := findOrphanTmps(filepath.Join(t.TempDir(), "nope")); len(orphans) != 0 {
		t.Fatalf("expected nil orphans for missing dir, got %v", orphans)
	}
}

// ---------------------------------------------------------------------------
// Run: end-to-end without --fix
// ---------------------------------------------------------------------------

func TestRun_StateFileNotFound(t *testing.T) {
	dir := t.TempDir()
	rep, err := Run(context.Background(), testDeps(), Options{OutputDir: dir})
	if err == nil {
		t.Fatal("expected error for missing state file")
	}
	if rep != nil {
		t.Errorf("expected nil report, got %+v", rep)
	}
	if !strings.Contains(err.Error(), "nothing to check") {
		t.Errorf("error = %v, want 'nothing to check'", err)
	}
}

func TestRun_StateFileUnreadable(t *testing.T) {
	// A directory at the state-file path triggers a read error that is not
	// IsNotExist, exercising the generic "cannot read state file" branch.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, stateFileName), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), testDeps(), Options{OutputDir: dir})
	if err == nil {
		t.Fatal("expected error reading directory as state file")
	}
	if !strings.Contains(err.Error(), "cannot read state file") {
		t.Errorf("error = %v, want 'cannot read state file'", err)
	}
}

func TestRun_CorruptJSONNoFix(t *testing.T) {
	dir := t.TempDir()
	writeRawState(t, dir, []byte("{not valid json"))
	rep, err := Run(context.Background(), testDeps(), Options{OutputDir: dir})
	if err == nil {
		t.Fatal("expected error for corrupt JSON without --fix")
	}
	if rep != nil {
		t.Errorf("expected nil report, got %+v", rep)
	}
	if !strings.Contains(err.Error(), "use --fix") {
		t.Errorf("error = %v, want hint to use --fix", err)
	}
}

func TestRun_CorruptJSONWithFix(t *testing.T) {
	dir := t.TempDir()
	writeRawState(t, dir, []byte("garbage{"))
	rep, err := Run(context.Background(), testDeps(), Options{OutputDir: dir, Fix: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep == nil || len(rep.Issues) != 1 {
		t.Fatalf("expected one reset issue, got %+v", rep)
	}
	if !strings.Contains(rep.Issues[0].Detail, "reset") {
		t.Errorf("Detail = %q, want 'reset'", rep.Issues[0].Detail)
	}
	// State file must now be valid empty JSON.
	s := readState(t, dir)
	if s.Completed == nil || len(s.Completed) != 0 {
		t.Errorf("expected empty Completed map, got %+v", s.Completed)
	}
	// A backup of the corrupt file must exist.
	entries, _ := os.ReadDir(dir)
	foundBackup := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt.") {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Errorf("expected a .corrupt. backup file in %v", dir)
	}
}

func TestRun_HealthyState(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "s1e1.mkv", 100)
	mkfile(t, dir, "s1e2.mkv", 200)
	writeStateFile(t, dir, domain.DownloadState{
		Series:   domain.SeriesID("42"),
		Metadata: &domain.SeriesMetadata{Title: "My Show"},
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Season: 1, Episode: 1, Path: "s1e1.mkv", Bytes: 100},
			"S1E2": {Season: 1, Episode: 2, Path: "s1e2.mkv", Bytes: 200},
		},
	})

	rep, err := Run(context.Background(), testDeps(), Options{OutputDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.SeriesID != "42" {
		t.Errorf("SeriesID = %q, want 42", rep.SeriesID)
	}
	if rep.SeriesTitle != "My Show" {
		t.Errorf("SeriesTitle = %q, want My Show", rep.SeriesTitle)
	}
	if rep.TotalInState != 2 {
		t.Errorf("TotalInState = %d, want 2", rep.TotalInState)
	}
	if rep.Healthy != 2 {
		t.Errorf("Healthy = %d, want 2", rep.Healthy)
	}
	if rep.HasIssues() {
		t.Errorf("expected no issues, got %+v", rep.Issues)
	}
}

func TestRun_MixedIssuesSortedByKey(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "s1e2.mkv", 50)  // truncated (recorded 100)
	mkfile(t, dir, "s1e3.mkv", 100) // healthy
	writeStateFile(t, dir, domain.DownloadState{
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Season: 1, Episode: 1, Path: "missing.mkv", Bytes: 100}, // missing
			"S1E2": {Season: 1, Episode: 2, Path: "s1e2.mkv", Bytes: 100},    // truncated
			"S1E3": {Season: 1, Episode: 3, Path: "s1e3.mkv", Bytes: 100},    // healthy
		},
	})

	rep, err := Run(context.Background(), testDeps(), Options{OutputDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1", rep.Healthy)
	}
	if len(rep.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %+v", len(rep.Issues), rep.Issues)
	}
	// Issues must be sorted by Key.
	if !sort.SliceIsSorted(rep.Issues, func(i, j int) bool {
		return rep.Issues[i].Key < rep.Issues[j].Key
	}) {
		t.Errorf("issues not sorted by key: %+v", rep.Issues)
	}
	if rep.Issues[0].Key != "S1E1" || rep.Issues[0].Kind != IssueMissing {
		t.Errorf("issue[0] = %+v, want S1E1 MISSING", rep.Issues[0])
	}
	if rep.Issues[1].Key != "S1E2" || rep.Issues[1].Kind != IssueTruncated {
		t.Errorf("issue[1] = %+v, want S1E2 TRUNCATED", rep.Issues[1])
	}
}

func TestRun_OrphanTmpAppended(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "s1e1.mkv", 100)
	mkfile(t, dir, "orphan.mkv.tmp", 10)
	writeStateFile(t, dir, domain.DownloadState{
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Path: "s1e1.mkv", Bytes: 100},
		},
	})

	rep, err := Run(context.Background(), testDeps(), Options{OutputDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.OrphanTmps) != 1 {
		t.Fatalf("expected 1 orphan tmp, got %v", rep.OrphanTmps)
	}
	// Orphan tmp must also appear as an issue.
	found := false
	for _, iss := range rep.Issues {
		if iss.Kind == IssueOrphanTmp && strings.HasSuffix(iss.Detail, "orphan.mkv.tmp") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected orphan tmp issue, got %+v", rep.Issues)
	}
}

func TestRun_NilCompletedMap(t *testing.T) {
	// State with explicit null completed must be normalized to empty.
	dir := t.TempDir()
	writeRawState(t, dir, []byte(`{"series":"7","completed":null}`))
	rep, err := Run(context.Background(), testDeps(), Options{OutputDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.TotalInState != 0 || rep.Healthy != 0 {
		t.Errorf("expected empty report, got total=%d healthy=%d", rep.TotalInState, rep.Healthy)
	}
	if rep.HasIssues() {
		t.Errorf("expected no issues, got %+v", rep.Issues)
	}
}

// ---------------------------------------------------------------------------
// Run with --fix
// ---------------------------------------------------------------------------

func TestRun_FixRemovesBrokenEntries(t *testing.T) {
	dir := t.TempDir()
	truncated := mkfile(t, dir, "s1e2.mkv", 50)
	mkfile(t, dir, "s1e3.mkv", 100) // healthy
	writeStateFile(t, dir, domain.DownloadState{
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Path: "missing.mkv", Bytes: 100}, // missing → removed
			"S1E2": {Path: "s1e2.mkv", Bytes: 100},    // truncated → removed + file deleted
			"S1E3": {Path: "s1e3.mkv", Bytes: 100},    // healthy → kept
			"S1E4": {Path: "", Bytes: 0},              // no path → removed
		},
	})

	_, err := Run(context.Background(), testDeps(), Options{OutputDir: dir, Fix: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := readState(t, dir)
	if len(s.Completed) != 1 {
		t.Fatalf("expected 1 surviving entry, got %d: %+v", len(s.Completed), s.Completed)
	}
	if _, ok := s.Completed["S1E3"]; !ok {
		t.Errorf("expected S1E3 to survive, got %+v", s.Completed)
	}
	// Truncated file must have been deleted from disk.
	if _, err := os.Stat(truncated); !os.IsNotExist(err) {
		t.Errorf("expected truncated file to be deleted, stat err = %v", err)
	}
}

func TestRun_FixKeepsSizeMismatch(t *testing.T) {
	// SIZE_MISMATCH (file larger than recorded) is NOT auto-removed: a larger
	// file is likely complete, only the recorded byte count is stale.
	dir := t.TempDir()
	mkfile(t, dir, "s1e1.mkv", 200)
	writeStateFile(t, dir, domain.DownloadState{
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Path: "s1e1.mkv", Bytes: 100},
		},
	})

	_, err := Run(context.Background(), testDeps(), Options{OutputDir: dir, Fix: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := readState(t, dir)
	if _, ok := s.Completed["S1E1"]; !ok {
		t.Errorf("size-mismatch entry must be preserved, got %+v", s.Completed)
	}
}

func TestRun_FixCleanTmpDeletesOrphan(t *testing.T) {
	dir := t.TempDir()
	orphan := mkfile(t, dir, "orphan.mkv.tmp", 10)
	writeStateFile(t, dir, domain.DownloadState{
		Completed: map[string]domain.CompletedRec{},
	})

	_, err := Run(context.Background(), testDeps(), Options{OutputDir: dir, Fix: true, CleanTmp: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("expected orphan tmp to be deleted, stat err = %v", err)
	}
}

func TestRun_FixWithoutCleanTmpKeepsOrphan(t *testing.T) {
	dir := t.TempDir()
	orphan := mkfile(t, dir, "orphan.mkv.tmp", 10)
	writeStateFile(t, dir, domain.DownloadState{
		Completed: map[string]domain.CompletedRec{},
	})

	_, err := Run(context.Background(), testDeps(), Options{OutputDir: dir, Fix: true, CleanTmp: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("expected orphan tmp to be preserved without CleanTmp, stat err = %v", err)
	}
}

func TestRun_FixCleanTmpRemovesHlsDir(t *testing.T) {
	dir := t.TempDir()
	hlsDir := filepath.Join(dir, "ep1.ts.hls-tmp")
	if err := os.MkdirAll(filepath.Join(hlsDir, "seg"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hlsDir, "seg", "0.ts"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, dir, domain.DownloadState{Completed: map[string]domain.CompletedRec{}})

	_, err := Run(context.Background(), testDeps(), Options{OutputDir: dir, Fix: true, CleanTmp: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(hlsDir); !os.IsNotExist(err) {
		t.Errorf("expected hls-tmp dir to be removed recursively, stat err = %v", err)
	}
}

func TestRun_NoFixDoesNotMutateState(t *testing.T) {
	dir := t.TempDir()
	writeStateFile(t, dir, domain.DownloadState{
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Path: "missing.mkv", Bytes: 100},
		},
	})
	before := readState(t, dir)

	_, err := Run(context.Background(), testDeps(), Options{OutputDir: dir, Fix: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := readState(t, dir)
	if len(after.Completed) != len(before.Completed) {
		t.Errorf("state mutated without --fix: before=%d after=%d", len(before.Completed), len(after.Completed))
	}
}

// ---------------------------------------------------------------------------
// writeState / copyFile helpers
// ---------------------------------------------------------------------------

func TestWriteStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	state := domain.DownloadState{
		Series: domain.SeriesID("9"),
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Season: 1, Episode: 1, Path: "a.mkv", Bytes: 5, CompletedAt: time.Unix(0, 0).UTC()},
		},
	}
	if err := writeState(path, state); err != nil {
		t.Fatalf("writeState: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got domain.DownloadState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Series != "9" || got.Completed["S1E1"].Bytes != 5 {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	want := []byte("hello world")
	if err := os.WriteFile(src, want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("copied content = %q, want %q", got, want)
	}
}

func TestCopyFile_MissingSource(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Fatal("expected error copying missing source")
	}
}
