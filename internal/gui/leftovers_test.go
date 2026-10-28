package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// The real mismatch this whole lookup exists for: a card started from a search
// result carries the short title, while the folder on disk is named after the
// full one from the item page ("Рус / Eng", with the slash sanitized).
const (
	shortTitle  = "Страх и ненависть"
	folderTitle = "Страх и ненависть _ Fear and Loathing"
	itemURL     = "https://kino.watch/item/view/42"
	itemID      = "42"
)

// writeSized creates path (with parents) holding size bytes.
func writeSized(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedPartials lays out a series folder holding one interrupted HLS download (a
// segment directory), one interrupted progressive download (a part file), a
// finished episode, and partial data for an episode owned by someone else.
// Returns the series folder and its season folder.
func seedPartials(t *testing.T, root string) (string, string) {
	t.Helper()
	dir := filepath.Join(root, folderTitle)
	season := filepath.Join(dir, "Season 01")
	writeSized(t, filepath.Join(season, "S01E01.mkv.ts.hls-tmp", "seg_00000.ts"), 100)
	writeSized(t, filepath.Join(season, "S01E01.mkv.ts.hls-tmp", "seg_00001.ts"), 150)
	writeSized(t, filepath.Join(season, "S01E02.mkv.tmp"), 200)
	writeSized(t, filepath.Join(season, "S01E03.mkv"), 999)
	writeSized(t, filepath.Join(season, "S01E04.mkv.ts.hls-tmp", "seg_00000.ts"), 500)
	return dir, season
}

// seedStateFile drops the state file a finished episode leaves behind, which is
// how a card persisted before the engine reported its folder finds it again.
func seedStateFile(t *testing.T, dir string) {
	t.Helper()
	writeStateFile(t, dir, domain.DownloadState{
		Series:    domain.SeriesID(itemID),
		Metadata:  &domain.SeriesMetadata{Title: folderTitle, InputURL: itemURL},
		Completed: map[string]domain.CompletedRec{"S1E3": {}},
	})
}

func TestJobTempPaths_ScopedToOwnEpisodes(t *testing.T) {
	root := t.TempDir()
	dir, season := seedPartials(t, root)

	// E03 is listed but finished, E04 is partial but belongs to another job.
	stems := map[string]bool{"S01E01": true, "S01E02": true, "S01E03": true}
	got := jobTempPaths(dir, stems)

	want := map[string]bool{
		filepath.Join(season, "S01E01.mkv.ts.hls-tmp"): true,
		filepath.Join(season, "S01E02.mkv.tmp"):        true,
	}
	if len(got) != len(want) {
		t.Fatalf("found %d paths, want %d: %v", len(got), len(want), got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}

	var total int64
	for _, p := range got {
		total += pathSize(p)
	}
	if total != 450 { // 100 + 150 segments + a 200-byte part file
		t.Errorf("total = %d bytes, want 450", total)
	}
}

func TestJobTempPaths_NoFolderFindsNothing(t *testing.T) {
	root := t.TempDir()
	seedPartials(t, root)
	stems := map[string]bool{"S01E01": true}
	if got := jobTempPaths("", stems); got != nil {
		t.Errorf("an unresolved folder should find nothing, got %v", got)
	}
	if got := jobTempPaths(filepath.Join(root, "Other Show"), stems); got != nil {
		t.Errorf("another series' folder should find nothing, got %v", got)
	}
}

// pausedJob builds a paused job owning S01E01 and S01E02 of the seeded series,
// with the folder the engine reported (the normal case).
func pausedJob(id, root string) *Job {
	j := newJob(id, itemURL, domain.RunConfig{OutputPath: root})
	j.status = statusPaused
	j.title = shortTitle
	j.seriesDir = filepath.Join(root, folderTitle)
	j.episodes["S1E1"] = &EpisodeView{Key: "S1E1", Season: 1, Episode: 1, State: epPaused}
	j.episodes["S1E2"] = &EpisodeView{Key: "S1E2", Season: 1, Episode: 2, State: epPaused}
	return j
}

// A legacy run that paused before its first completed episode left a
// metadata-only state file (zero completed records) next to its partial
// segments. The library scan rightly skips such files — they are not downloads
// the user has — but the leftovers lookup must still see them, or removing the
// restored card reports 0 bytes and strands the segments.
func TestSeriesDirByItem_FindsMetadataOnlyStateFiles(t *testing.T) {
	root := t.TempDir()
	dir, _ := seedPartials(t, root)
	writeStateFile(t, dir, domain.DownloadState{
		Series:    domain.SeriesID(itemID),
		Metadata:  &domain.SeriesMetadata{Title: folderTitle, InputURL: itemURL},
		Completed: map[string]domain.CompletedRec{}, // nothing ever finished
	})

	if got := seriesDirByItem(root, itemURL); got != dir {
		t.Errorf("seriesDirByItem = %q, want the metadata-only folder %q", got, dir)
	}
}

// The bug this guards: deriving the folder from the card's display title lands
// on a directory that does not exist, the scan finds nothing, and the card is
// removed silently while gigabytes stay stranded on disk.
func TestScopeOf_UsesReportedFolderNotDisplayTitle(t *testing.T) {
	root := t.TempDir()
	dir, _ := seedPartials(t, root)

	sc := scopeOf(pausedJob("j", root))
	if sc.dir != dir {
		t.Fatalf("dir = %q, want the folder the engine reported (%q)", sc.dir, dir)
	}
	if got := len(jobTempPaths(sc.dir, sc.stems)); got != 2 {
		t.Errorf("found %d partial items, want 2 — a short title must not hide them", got)
	}
}

// A card persisted before the engine reported its folder has no recorded path,
// so it falls back to the item id in the state file.
func TestScopeOf_LegacyCardResolvesFolderByItemID(t *testing.T) {
	root := t.TempDir()
	dir, _ := seedPartials(t, root)
	seedStateFile(t, dir)

	j := pausedJob("legacy", root)
	j.seriesDir = ""

	sc := scopeOf(j)
	if sc.dir != dir {
		t.Fatalf("dir = %q, want %q resolved from the state file", sc.dir, dir)
	}
	if got := len(jobTempPaths(sc.dir, sc.stems)); got != 2 {
		t.Errorf("found %d partial items, want 2", got)
	}
}

func TestManagerRemove_PurgesOnlyWhenAsked(t *testing.T) {
	root := t.TempDir()
	_, season := seedPartials(t, root)
	hlsTmp := filepath.Join(season, "S01E01.mkv.ts.hls-tmp")
	partFile := filepath.Join(season, "S01E02.mkv.tmp")

	m := newJobManager(newHub())
	m.add(pausedJob("keep", root))
	if removed, _ := m.remove("keep", false); !removed {
		t.Fatal("paused job should be removable")
	}
	// The default stays conservative: the card goes, the data stays.
	if _, err := os.Stat(hlsTmp); err != nil {
		t.Errorf("remove without purge deleted the segment dir: %v", err)
	}

	m.add(pausedJob("purge", root))
	if removed, _ := m.remove("purge", true); !removed {
		t.Fatal("paused job should be removable")
	}
	if _, err := os.Stat(hlsTmp); !os.IsNotExist(err) {
		t.Errorf("purge left the segment dir behind: %v", err)
	}
	if _, err := os.Stat(partFile); !os.IsNotExist(err) {
		t.Errorf("purge left the part file behind: %v", err)
	}
	// Someone else's partial data and the finished episode are untouched.
	if _, err := os.Stat(filepath.Join(season, "S01E04.mkv.ts.hls-tmp")); err != nil {
		t.Errorf("purge reached another job's segments: %v", err)
	}
	if _, err := os.Stat(filepath.Join(season, "S01E03.mkv")); err != nil {
		t.Errorf("purge deleted a finished episode: %v", err)
	}
}

func TestLeftovers_ReportsBytesAndConflict(t *testing.T) {
	root := t.TempDir()
	_, season := seedPartials(t, root)

	m := newJobManager(newHub())
	m.add(pausedJob("a", root))

	got, ok := m.leftovers("a")
	if !ok {
		t.Fatal("leftovers: job not found")
	}
	if got.Bytes != 450 || got.Items != 2 {
		t.Errorf("leftovers = %+v, want 450 bytes over 2 items", got)
	}
	if got.Conflict {
		t.Error("a lone job has no one to conflict with")
	}

	// A second job that can still resume the same episodes owns this data too.
	m.add(pausedJob("b", root))
	got, _ = m.leftovers("a")
	if !got.Conflict {
		t.Error("a sibling job resuming the same episodes should be a conflict")
	}
	if freed, _ := m.purgeTemp(pausedJob("a", root)); freed != 0 {
		t.Errorf("purge freed %d bytes despite the conflict, want 0", freed)
	}
	if _, err := os.Stat(filepath.Join(season, "S01E01.mkv.ts.hls-tmp")); err != nil {
		t.Errorf("a conflicting purge deleted the segments anyway: %v", err)
	}
}

func TestLeftovers_UnknownJob(t *testing.T) {
	m := newJobManager(newHub())
	if _, ok := m.leftovers("nope"); ok {
		t.Error("leftovers should report an unknown job as missing")
	}
}

func TestJobTempPathsRecognizeQualitySuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "S01E02-1080p-h265.mkv.ts.hls-tmp")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	got := jobTempPaths(dir, map[string]bool{"S01E02": true})
	if len(got) != 1 || got[0] != path {
		t.Fatalf("quality cache lost: %v", got)
	}
}
