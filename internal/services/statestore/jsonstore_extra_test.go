package statestore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// readStateFile reads and unmarshals the state file at path; fails the test on error.
func readStateFile(t *testing.T, path string) domain.DownloadState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file %q: %v", path, err)
	}
	var st domain.DownloadState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal state file %q: %v", path, err)
	}
	return st
}

// TestStatePathFallbackToRoot verifies statePath uses rootDir until SetSeriesDir.
func TestStatePathFallbackToRoot(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())

	if got, want := store.statePath(), filepath.Join(root, stateFileName); got != want {
		t.Fatalf("statePath before SetSeriesDir = %q, want %q", got, want)
	}

	seriesDir := filepath.Join(root, "My Series")
	store.SetSeriesDir(seriesDir)
	if got, want := store.statePath(), filepath.Join(seriesDir, stateFileName); got != want {
		t.Fatalf("statePath after SetSeriesDir = %q, want %q", got, want)
	}
	if got, want := store.legacyStatePath(), filepath.Join(root, stateFileName); got != want {
		t.Fatalf("legacyStatePath = %q, want %q", got, want)
	}
}

// TestMarkCompletedCreatesSeriesDir verifies that MarkCompleted creates the
// series subdirectory on the first write (MkdirAll path).
func TestMarkCompletedCreatesSeriesDir(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	seriesDir := filepath.Join(root, "Some Show", "nested")
	store.SetSeriesDir(seriesDir)

	key := domain.EpisodeKey{Series: "s1", Season: 1, Episode: 1}
	if err := store.MarkCompleted(context.Background(), domain.CompletedInfo{Key: key, Bytes: 7}); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	statePath := filepath.Join(seriesDir, stateFileName)
	st := readStateFile(t, statePath)
	if _, ok := st.Completed["S1E1"]; !ok {
		t.Fatalf("expected S1E1 in state at %q", statePath)
	}
	// The legacy/root location must NOT have been written.
	if _, err := os.Stat(filepath.Join(root, stateFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no state file at root, stat err = %v", err)
	}
}

// TestLoadLegacyMigration verifies that when seriesDir has no state file but the
// legacy root location does, Load reads (migrates) from the legacy location.
func TestLoadLegacyMigration(t *testing.T) {
	root := t.TempDir()
	series := domain.SeriesID("legacy-series")

	// Write a legacy state file at root with one completed episode.
	legacy := domain.DownloadState{
		Series: series,
		Completed: map[string]domain.CompletedRec{
			"S1E2": {Season: 1, Episode: 2, Path: "/old/ep.mkv", Bytes: 99},
		},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, stateFileName), data, 0644); err != nil {
		t.Fatal(err)
	}

	store := New(root, testLogger())
	seriesDir := filepath.Join(root, "Legacy Series")
	if err := os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatal(err)
	}
	store.SetSeriesDir(seriesDir)

	got, err := store.Load(context.Background(), series)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rec, ok := got.Completed["S1E2"]
	if !ok {
		t.Fatalf("expected S1E2 migrated from legacy, got %#v", got.Completed)
	}
	if rec.Path != "/old/ep.mkv" || rec.Bytes != 99 {
		t.Fatalf("legacy rec not preserved: %#v", rec)
	}
}

// TestLoadLegacyMigrationWrongSeries verifies migration returns empty when the
// legacy file is for a different series.
func TestLoadLegacyMigrationWrongSeries(t *testing.T) {
	root := t.TempDir()
	legacy := domain.DownloadState{
		Series:    "other-series",
		Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}},
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(filepath.Join(root, stateFileName), data, 0644); err != nil {
		t.Fatal(err)
	}

	store := New(root, testLogger())
	store.SetSeriesDir(filepath.Join(root, "Wanted Series"))

	got, err := store.Load(context.Background(), "wanted-series")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Completed) != 0 {
		t.Fatalf("expected empty for mismatched legacy series, got %d", len(got.Completed))
	}
	if got.Completed == nil {
		t.Fatal("Completed must not be nil")
	}
	if got.Series != "wanted-series" {
		t.Fatalf("expected series wanted-series, got %q", got.Series)
	}
}

// TestLoadLegacyCorrupt verifies a corrupt legacy file degrades to empty.
func TestLoadLegacyCorrupt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, stateFileName), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	store := New(root, testLogger())
	store.SetSeriesDir(filepath.Join(root, "Series X"))

	got, err := store.Load(context.Background(), "series-x")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Completed) != 0 || got.Completed == nil {
		t.Fatalf("expected empty non-nil map, got %#v", got.Completed)
	}
}

// TestSeriesDirTakesPrecedenceOverLegacy verifies that when the series dir HAS a
// state file, the legacy location is ignored.
func TestSeriesDirTakesPrecedenceOverLegacy(t *testing.T) {
	root := t.TempDir()
	series := domain.SeriesID("dual")

	// legacy at root
	legacy := domain.DownloadState{Series: series, Completed: map[string]domain.CompletedRec{"S9E9": {Season: 9, Episode: 9}}}
	ld, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(filepath.Join(root, stateFileName), ld, 0644); err != nil {
		t.Fatal(err)
	}

	seriesDir := filepath.Join(root, "Dual")
	if err := os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// series-dir state
	cur := domain.DownloadState{Series: series, Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}}}
	cd, _ := json.MarshalIndent(cur, "", "  ")
	if err := os.WriteFile(filepath.Join(seriesDir, stateFileName), cd, 0644); err != nil {
		t.Fatal(err)
	}

	store := New(root, testLogger())
	store.SetSeriesDir(seriesDir)

	got, err := store.Load(context.Background(), series)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := got.Completed["S1E1"]; !ok {
		t.Fatal("expected series-dir state S1E1")
	}
	if _, ok := got.Completed["S9E9"]; ok {
		t.Fatal("legacy S9E9 must NOT appear when series-dir state exists")
	}
}

// TestSetMetadataPersistsAndPreservesCompleted verifies SetMetadata writes
// metadata while preserving previously completed episodes (load-modify-write).
func TestSetMetadataPersistsAndPreservesCompleted(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	ctx := context.Background()
	series := domain.SeriesID("meta")

	key := domain.EpisodeKey{Series: series, Season: 3, Episode: 4}
	if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: key, Bytes: 11}); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	meta := domain.SeriesMetadata{
		Title:       "Метаданные",
		Description: "desc",
		InputURL:    "https://kino.pub/item/1",
		Type:        "serial",
		Genres:      []string{"drama", "комедия"},
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	if err := store.SetMetadata(ctx, series, meta); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}

	got, err := store.Load(ctx, series)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Metadata == nil {
		t.Fatal("expected metadata to be set")
	}
	if got.Metadata.Title != "Метаданные" || got.Metadata.Type != "serial" {
		t.Fatalf("metadata not persisted: %#v", got.Metadata)
	}
	if len(got.Metadata.Genres) != 2 {
		t.Fatalf("expected 2 genres, got %#v", got.Metadata.Genres)
	}
	// Completed episode must survive the metadata write.
	if _, ok := got.Completed["S3E4"]; !ok {
		t.Fatalf("SetMetadata dropped completed episode: %#v", got.Completed)
	}
}

// TestMarkCompletedPreservesMetadata verifies that marking a new episode does
// not wipe metadata set earlier.
func TestMarkCompletedPreservesMetadata(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	ctx := context.Background()
	series := domain.SeriesID("keepmeta")

	if err := store.SetMetadata(ctx, series, domain.SeriesMetadata{Title: "T"}); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	key := domain.EpisodeKey{Series: series, Season: 1, Episode: 1}
	if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: key}); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	got, _ := store.Load(ctx, series)
	if got.Metadata == nil || got.Metadata.Title != "T" {
		t.Fatalf("metadata lost after MarkCompleted: %#v", got.Metadata)
	}
}

// TestMarkCompletedOverwritesSameKey verifies remarking the same episode
// updates the record rather than duplicating it.
func TestMarkCompletedOverwritesSameKey(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	ctx := context.Background()
	key := domain.EpisodeKey{Series: "ow", Season: 1, Episode: 1}

	if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: key, Path: "/a", Bytes: 1, Quality: "480p"}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: key, Path: "/b", Bytes: 2, Quality: "1080p"}); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Load(ctx, "ow")
	if len(got.Completed) != 1 {
		t.Fatalf("expected 1 entry after overwrite, got %d", len(got.Completed))
	}
	rec := got.Completed["S1E1"]
	if rec.Path != "/b" || rec.Bytes != 2 || rec.Quality != "1080p" {
		t.Fatalf("record not overwritten: %#v", rec)
	}
}

// TestMarkCompletedSetsCompletedAt verifies CompletedAt timestamp is recorded.
func TestMarkCompletedSetsCompletedAt(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	before := time.Now()
	key := domain.EpisodeKey{Series: "ts", Season: 1, Episode: 1}
	if err := store.MarkCompleted(context.Background(), domain.CompletedInfo{Key: key}); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	got, _ := store.Load(context.Background(), "ts")
	rec := got.Completed["S1E1"]
	if rec.CompletedAt.Before(before) || rec.CompletedAt.After(after) {
		t.Fatalf("CompletedAt %v not within [%v, %v]", rec.CompletedAt, before, after)
	}
}

// TestMarkCompletedAllFieldsRoundTrip verifies every CompletedInfo field maps to
// the persisted CompletedRec.
func TestMarkCompletedAllFieldsRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	key := domain.EpisodeKey{Series: "full", Season: 7, Episode: 12}
	info := domain.CompletedInfo{
		Key:        key,
		Path:       "/p/S07E12.mkv",
		Bytes:      9876543210,
		Title:      "Эпизод",
		Quality:    "2160p",
		Resolution: "3840x2160",
		BitRate:    15000,
		PageLink:   "https://kino.pub/v/abc",
		MediaURL:   "https://cdn/x.m3u8",
	}
	if err := store.MarkCompleted(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load(context.Background(), "full")
	rec := got.Completed["S7E12"]
	if rec.Season != 7 || rec.Episode != 12 {
		t.Errorf("season/episode = %d/%d", rec.Season, rec.Episode)
	}
	if rec.Path != info.Path || rec.Bytes != info.Bytes || rec.Title != info.Title {
		t.Errorf("path/bytes/title mismatch: %#v", rec)
	}
	if rec.Quality != info.Quality || rec.Resolution != info.Resolution || rec.BitRate != info.BitRate {
		t.Errorf("quality/resolution/bitrate mismatch: %#v", rec)
	}
	if rec.PageLink != info.PageLink || rec.MediaURL != info.MediaURL {
		t.Errorf("links mismatch: %#v", rec)
	}
}

// TestIsCompletedNilMap verifies IsCompleted is safe on a zero-value state.
func TestIsCompletedNilMap(t *testing.T) {
	store := New(t.TempDir(), testLogger())
	var st domain.DownloadState // Completed is nil
	if store.IsCompleted(st, domain.EpisodeKey{Season: 1, Episode: 1}) {
		t.Fatal("expected false for nil completed map")
	}
}

// TestEpisodeKeyStringEdge covers zero and negative season/episode formatting.
func TestEpisodeKeyStringEdge(t *testing.T) {
	cases := []struct {
		k    domain.EpisodeKey
		want string
	}{
		{domain.EpisodeKey{Season: 0, Episode: 0}, "S0E0"},
		{domain.EpisodeKey{Season: -1, Episode: -2}, "S-1E-2"},
		{domain.EpisodeKey{Season: 100, Episode: 999}, "S100E999"},
	}
	for _, c := range cases {
		if got := episodeKeyString(c.k); got != c.want {
			t.Errorf("episodeKeyString(%v) = %q, want %q", c.k, got, c.want)
		}
	}
}

// TestConcurrentMarkCompletedSameFile verifies that concurrent JSONStore
// instances writing the same file via the shared path mutex do not drop
// completions (the documented purpose of pathMutexes).
func TestConcurrentMarkCompletedSameFile(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Race Series")
	if err := os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatal(err)
	}
	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine builds its own JSONStore over the same file.
			s := New(root, testLogger())
			s.SetSeriesDir(seriesDir)
			key := domain.EpisodeKey{Series: "race", Season: 1, Episode: i + 1}
			if err := s.MarkCompleted(context.Background(), domain.CompletedInfo{Key: key, Bytes: int64(i)}); err != nil {
				t.Errorf("MarkCompleted: %v", err)
			}
		}(i)
	}
	wg.Wait()

	reader := New(root, testLogger())
	reader.SetSeriesDir(seriesDir)
	got, err := reader.Load(context.Background(), "race")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Completed) != n {
		t.Fatalf("expected %d completions after concurrent writes, got %d", n, len(got.Completed))
	}
}

// TestConcurrentLoadDuringWrites verifies Load always returns a complete
// (non-error, non-nil-map) state while concurrent writes happen.
func TestConcurrentLoadDuringWrites(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 30; i++ {
			key := domain.EpisodeKey{Series: "cl", Season: 1, Episode: i + 1}
			_ = store.MarkCompleted(ctx, domain.CompletedInfo{Key: key})
		}
		close(done)
	}()

	for {
		select {
		case <-done:
			st, err := store.Load(ctx, "cl")
			if err != nil {
				t.Fatalf("final Load error: %v", err)
			}
			if st.Completed == nil {
				t.Fatal("Completed nil")
			}
			return
		default:
			st, err := store.Load(ctx, "cl")
			if err != nil {
				t.Fatalf("Load error during writes: %v", err)
			}
			if st.Completed == nil {
				t.Fatal("Completed map nil during writes")
			}
		}
	}
}

// TestSetMetadataNilGenresOmitted verifies the JSON shape: nil genres omitted.
func TestSetMetadataReplacesMetadata(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	ctx := context.Background()
	series := domain.SeriesID("replace")

	if err := store.SetMetadata(ctx, series, domain.SeriesMetadata{Title: "First"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMetadata(ctx, series, domain.SeriesMetadata{Title: "Second", Type: "movie"}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load(ctx, series)
	if got.Metadata == nil || got.Metadata.Title != "Second" || got.Metadata.Type != "movie" {
		t.Fatalf("metadata not replaced: %#v", got.Metadata)
	}
}

// TestAtomicWriteNoTempLeftovers verifies that after writes only the state file
// (no .tmp-* leftovers) remains.
func TestAtomicWriteNoTempLeftovers(t *testing.T) {
	root := t.TempDir()
	store := New(root, testLogger())
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		key := domain.EpisodeKey{Series: "atom", Season: 1, Episode: i + 1}
		if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != stateFileName {
			t.Errorf("unexpected leftover file %q", e.Name())
		}
	}
}
