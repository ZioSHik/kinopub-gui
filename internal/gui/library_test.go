package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestIsSerialType(t *testing.T) {
	cases := map[string]bool{
		"serial":     true,
		"docuserial": true,
		"tvshow":     true,
		"SERIAL":     true, // case-insensitive
		"movie":      false,
		"documovie":  false,
		"4k":         false,
		"":           false,
	}
	for typ, want := range cases {
		if got := isSerialType(typ); got != want {
			t.Errorf("isSerialType(%q) = %v, want %v", typ, got, want)
		}
	}
}

func TestSeriesMatchesItem(t *testing.T) {
	t.Run("by series id", func(t *testing.T) {
		s := LibrarySeries{SeriesID: "42"}
		if !seriesMatchesItem(s, "42") {
			t.Error("should match by SeriesID")
		}
		if seriesMatchesItem(s, "99") {
			t.Error("should not match a different id")
		}
	})
	t.Run("by input url", func(t *testing.T) {
		s := LibrarySeries{InputURL: "https://kino.watch/item/view/77"}
		if !seriesMatchesItem(s, "77") {
			t.Error("should match the id embedded in the InputURL")
		}
		if seriesMatchesItem(s, "78") {
			t.Error("should not match wrong id")
		}
	})
	t.Run("no id, no url", func(t *testing.T) {
		if seriesMatchesItem(LibrarySeries{}, "1") {
			t.Error("empty series must not match")
		}
	})
}

// writeStateFile writes a full DownloadState into dir/.kinopub-state.json.
func writeStateFile(t *testing.T, dir string, state domain.DownloadState) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, stateFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadLibraryState(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "My Show")
	// One existing file, one missing.
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "s1e1.mkv"), []byte("12345"), 0o644)

	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	state := domain.DownloadState{
		Series: "42",
		Metadata: &domain.SeriesMetadata{
			Title:         "Pretty Title",
			OriginalTitle: "Orig",
			Type:          "serial",
			Genres:        []string{"Drama"},
		},
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Season: 1, Episode: 1, Path: "s1e1.mkv", Bytes: 5, CompletedAt: t0},
			"S1E2": {Season: 1, Episode: 2, Path: "missing.mkv", Bytes: 10, CompletedAt: t1},
		},
	}
	writeStateFile(t, dir, state)

	got, ok := readLibraryState(filepath.Join(dir, stateFileName))
	if !ok {
		t.Fatal("readLibraryState returned ok=false")
	}
	if got.Title != "Pretty Title" || got.SeriesID != "42" || got.Type != "serial" {
		t.Errorf("metadata not applied: %+v", got)
	}
	// Both records are counted, but only the 5 bytes actually on disk: S1E2's
	// file is gone, so charging its 10 bytes would claim space the entry no
	// longer occupies.
	if got.Count != 2 || got.TotalBytes != 5 {
		t.Errorf("count/bytes = %d/%d, want 2/5", got.Count, got.TotalBytes)
	}
	// UpdatedAt is the latest CompletedAt.
	if !got.UpdatedAt.Equal(t1) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, t1)
	}
	// Episodes sorted by season/episode; Exists reflects the file presence.
	if got.Episodes[0].Key != "S1E1" || !got.Episodes[0].Exists {
		t.Errorf("S1E1 should exist: %+v", got.Episodes[0])
	}
	if got.Episodes[1].Key != "S1E2" || got.Episodes[1].Exists {
		t.Errorf("S1E2 should be missing: %+v", got.Episodes[1])
	}
	// A serial is not a movie.
	if got.IsMovie {
		t.Error("serial should not be classified as a movie")
	}
}

func TestReadLibraryStateDoesNotTreatEmptyFilesOrDirectoriesAsDownloaded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.mkv"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.mkv"), 0700); err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, dir, domain.DownloadState{Series: "42", Completed: map[string]domain.CompletedRec{
		"S1E1": {Season: 1, Episode: 1, Path: "empty.mkv", Bytes: 100},
		"S1E2": {Season: 1, Episode: 2, Path: "directory.mkv", Bytes: 100},
	}})
	got, ok := readLibraryState(filepath.Join(dir, stateFileName))
	if !ok || len(got.Episodes) != 2 || got.TotalBytes != 0 {
		t.Fatalf("missing-file history should remain visible without claiming disk usage: %+v", got)
	}
	for _, ep := range got.Episodes {
		if ep.Exists {
			t.Errorf("unusable output must be offered for downloading again: %+v", ep)
		}
	}
}

func TestReadLibraryState_TitleFallsBackToDirName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "FolderName")
	writeStateFile(t, dir, domain.DownloadState{
		Series:    "1",
		Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}},
	})
	got, ok := readLibraryState(filepath.Join(dir, stateFileName))
	if !ok {
		t.Fatal("ok=false")
	}
	if got.Title != "FolderName" {
		t.Errorf("Title fallback = %q, want FolderName", got.Title)
	}
}

func TestReadLibraryState_BadFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "bad")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, stateFileName), []byte("{not json"), 0o644)
	if _, ok := readLibraryState(filepath.Join(dir, stateFileName)); ok {
		t.Error("malformed state file should return ok=false")
	}
	if _, ok := readLibraryState(filepath.Join(dir, "does-not-exist.json")); ok {
		t.Error("missing file should return ok=false")
	}
}

func TestScanLibrary(t *testing.T) {
	root := t.TempDir()
	// Two series, one nested; a hidden dir is skipped.
	writeStateFile(t, filepath.Join(root, "A"), domain.DownloadState{
		Series:    "1",
		Metadata:  &domain.SeriesMetadata{UpdatedAt: time.Unix(100, 0)},
		Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}},
	})
	writeStateFile(t, filepath.Join(root, "nested", "B"), domain.DownloadState{
		Series:    "2",
		Metadata:  &domain.SeriesMetadata{UpdatedAt: time.Unix(200, 0)},
		Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}},
	})
	// Hidden directory containing a state file must be skipped.
	writeStateFile(t, filepath.Join(root, ".hidden"), domain.DownloadState{
		Series:    "3",
		Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}},
	})

	resp := scanLibrary([]string{root, ""}) // "" root is skipped
	if len(resp.Series) != 2 {
		t.Fatalf("want 2 series (hidden skipped), got %d", len(resp.Series))
	}
	// Sorted by UpdatedAt descending → B (200) before A (100).
	if resp.Series[0].SeriesID != "2" {
		t.Errorf("expected newest first; got %q", resp.Series[0].SeriesID)
	}
}

func TestScanLibrary_NilDirsReturnsEmptyNonNil(t *testing.T) {
	resp := scanLibrary(nil)
	if resp.Series == nil || resp.Dirs == nil {
		t.Errorf("nil dirs should yield non-nil empty slices: %+v", resp)
	}
	if len(resp.Series) != 0 {
		t.Errorf("expected no series, got %d", len(resp.Series))
	}
}

func TestDownloadedForItem(t *testing.T) {
	root := t.TempDir()
	writeStateFile(t, filepath.Join(root, "Show"), domain.DownloadState{
		Series: "55",
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Season: 1, Episode: 1, Resolution: "1080p"},
		},
	})

	resp := downloadedForItem([]string{root}, "55")
	if resp.ID != "55" {
		t.Errorf("ID = %q", resp.ID)
	}
	if len(resp.Episodes) != 1 || resp.Episodes[0].Resolution != "1080p" {
		t.Errorf("episodes = %+v", resp.Episodes)
	}
	if resp.Dir == "" {
		t.Error("Dir should be set to the matching series folder")
	}

	// Empty id → empty, non-nil episodes.
	empty := downloadedForItem([]string{root}, "")
	if empty.Episodes == nil || len(empty.Episodes) != 0 {
		t.Errorf("empty id should yield empty episodes, got %+v", empty.Episodes)
	}
	// Unknown id → no episodes.
	none := downloadedForItem([]string{root}, "999")
	if len(none.Episodes) != 0 {
		t.Errorf("unknown id should yield no episodes, got %+v", none.Episodes)
	}
}

func TestResolveLibraryDir(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "Series")
	_ = os.MkdirAll(good, 0o755)
	_ = os.WriteFile(filepath.Join(good, stateFileName), []byte("{}"), 0o644)

	t.Run("valid inside root", func(t *testing.T) {
		abs, err := resolveLibraryDir(good, []string{root})
		if err != nil {
			t.Fatalf("expected ok, got %v", err)
		}
		if abs != filepath.Clean(good) {
			t.Errorf("abs = %q, want %q", abs, good)
		}
	})
	t.Run("root itself rejected", func(t *testing.T) {
		_ = os.WriteFile(filepath.Join(root, stateFileName), []byte("{}"), 0o644)
		if _, err := resolveLibraryDir(root, []string{root}); err == nil {
			t.Error("the root itself must be rejected")
		}
	})
	t.Run("no state file rejected", func(t *testing.T) {
		nostate := filepath.Join(root, "Empty")
		_ = os.MkdirAll(nostate, 0o755)
		if _, err := resolveLibraryDir(nostate, []string{root}); err == nil {
			t.Error("a folder without a state file must be rejected")
		}
	})
	t.Run("outside roots rejected", func(t *testing.T) {
		other := t.TempDir()
		if _, err := resolveLibraryDir(good, []string{other}); err == nil {
			t.Error("a folder outside the configured roots must be rejected")
		}
	})
}

func TestLibraryItemID(t *testing.T) {
	cases := []struct {
		name string
		in   LibrarySeries
		want string
	}{
		{"input url wins", LibrarySeries{SeriesID: "42", InputURL: "https://kino.watch/item/view/77"}, "77"},
		{"falls back to series id", LibrarySeries{SeriesID: "42"}, "42"},
		{"unparsable url falls back", LibrarySeries{SeriesID: "42", InputURL: "https://example.com/x"}, "42"},
		{"nothing to go on", LibrarySeries{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := libraryItemID(c.in); got != c.want {
				t.Errorf("libraryItemID() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPersistLibraryMetadata(t *testing.T) {
	updated := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)

	newState := func(meta *domain.SeriesMetadata) domain.DownloadState {
		return domain.DownloadState{
			Series:   "42",
			Metadata: meta,
			Completed: map[string]domain.CompletedRec{
				"S1E1": {Season: 1, Episode: 1, Path: "s1e1.mkv", Bytes: 5},
			},
		}
	}
	readBack := func(t *testing.T, dir string) domain.DownloadState {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, stateFileName))
		if err != nil {
			t.Fatal(err)
		}
		var got domain.DownloadState
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	t.Run("fills missing genres and type", func(t *testing.T) {
		dir := t.TempDir()
		writeStateFile(t, dir, newState(&domain.SeriesMetadata{Title: "Show", UpdatedAt: updated}))

		if err := persistLibraryMetadata(filepath.Join(dir, stateFileName), []string{"Drama"}, "serial"); err != nil {
			t.Fatal(err)
		}

		got := readBack(t, dir)
		if len(got.Metadata.Genres) != 1 || got.Metadata.Genres[0] != "Drama" {
			t.Errorf("genres = %v, want [Drama]", got.Metadata.Genres)
		}
		if got.Metadata.Type != "serial" {
			t.Errorf("type = %q, want serial", got.Metadata.Type)
		}
		// Everything else must survive: UpdatedAt orders the library, and the
		// completed records are the library entry itself.
		if !got.Metadata.UpdatedAt.Equal(updated) {
			t.Errorf("UpdatedAt = %v, want %v", got.Metadata.UpdatedAt, updated)
		}
		if got.Metadata.Title != "Show" {
			t.Errorf("Title = %q, want Show", got.Metadata.Title)
		}
		if len(got.Completed) != 1 {
			t.Errorf("completed records = %d, want 1", len(got.Completed))
		}
	})

	t.Run("never overwrites what is already recorded", func(t *testing.T) {
		dir := t.TempDir()
		writeStateFile(t, dir, newState(&domain.SeriesMetadata{
			Title:  "Show",
			Type:   "movie",
			Genres: []string{"Comedy"},
		}))

		if err := persistLibraryMetadata(filepath.Join(dir, stateFileName), []string{"Drama"}, "serial"); err != nil {
			t.Fatal(err)
		}

		got := readBack(t, dir)
		if got.Metadata.Type != "movie" {
			t.Errorf("type = %q, want the recorded movie", got.Metadata.Type)
		}
		if len(got.Metadata.Genres) != 1 || got.Metadata.Genres[0] != "Comedy" {
			t.Errorf("genres = %v, want the recorded [Comedy]", got.Metadata.Genres)
		}
	})

	t.Run("leaves a state file without metadata alone", func(t *testing.T) {
		dir := t.TempDir()
		writeStateFile(t, dir, newState(nil))

		if err := persistLibraryMetadata(filepath.Join(dir, stateFileName), []string{"Drama"}, "serial"); err != nil {
			t.Fatal(err)
		}
		if got := readBack(t, dir); got.Metadata != nil {
			t.Errorf("metadata = %+v, want nil (nothing to attach to)", got.Metadata)
		}
	})

	t.Run("reports a missing state file", func(t *testing.T) {
		if err := persistLibraryMetadata(filepath.Join(t.TempDir(), stateFileName), nil, "movie"); err == nil {
			t.Error("want an error for a state file that does not exist")
		}
	})
}

// A state file with no completed episodes is not a library entry: it is what a
// run that resolved and then downloaded nothing leaves behind, and listing it
// showed a title the user never downloaded (filed under "movies", since the
// zero-episode fallback in isMovieDownload reads it as a single-part download).
func TestReadLibraryState_SkipsStateWithoutCompletedEpisodes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Never Downloaded")
	writeStateFile(t, dir, domain.DownloadState{
		Series: "42",
		Metadata: &domain.SeriesMetadata{
			Title:     "Never Downloaded",
			Type:      "serial",
			UpdatedAt: time.Unix(500, 0),
		},
		Completed: map[string]domain.CompletedRec{},
	})
	if _, ok := readLibraryState(filepath.Join(dir, stateFileName)); ok {
		t.Error("a state file with no completed episodes must not become a library entry")
	}
}

func TestScanLibrary_SkipsSeriesWithoutCompletedEpisodes(t *testing.T) {
	root := t.TempDir()
	writeStateFile(t, filepath.Join(root, "Downloaded"), domain.DownloadState{
		Series:    "1",
		Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}},
	})
	// Metadata only — a download that never produced a file.
	writeStateFile(t, filepath.Join(root, "Started Only"), domain.DownloadState{
		Series:    "2",
		Metadata:  &domain.SeriesMetadata{Title: "Started Only"},
		Completed: map[string]domain.CompletedRec{},
	})
	// Missing "completed" key entirely (nil map after unmarshal).
	writeStateFile(t, filepath.Join(root, "No Completed Key"), domain.DownloadState{Series: "3"})

	resp := scanLibrary([]string{root})
	if len(resp.Series) != 1 {
		t.Fatalf("want only the series with a completed episode, got %d: %+v", len(resp.Series), resp.Series)
	}
	if resp.Series[0].SeriesID != "1" {
		t.Errorf("wrong entry kept: %q", resp.Series[0].SeriesID)
	}
	// The item lookup is built on the same scan, so it must agree.
	if eps := downloadedForItem([]string{root}, "2"); len(eps.Episodes) != 0 {
		t.Errorf("an empty download must report no episodes, got %+v", eps.Episodes)
	}
}

// The voiceover an episode came out in has to survive the trip from the state
// file to the card, together with the flag saying it was a substitute.
func TestDownloadedForItem_CarriesVoiceover(t *testing.T) {
	root := t.TempDir()
	writeStateFile(t, filepath.Join(root, "Show"), domain.DownloadState{
		Series: "77",
		Completed: map[string]domain.CompletedRec{
			"S1E1": {Season: 1, Episode: 1, Audio: []string{"01. Многоголосый. AniLibria (RUS)"}},
			"S1E2": {Season: 1, Episode: 2, Audio: []string{"02. Двухголосый (RUS)"}, AudioFallback: true},
		},
	})

	byKey := map[string]DownloadedEpisode{}
	for _, ep := range downloadedForItem([]string{root}, "77").Episodes {
		byKey[ep.Key] = ep
	}

	first, ok := byKey["S1E1"]
	if !ok || len(first.Audio) != 1 || first.Audio[0] != "01. Многоголосый. AniLibria (RUS)" {
		t.Errorf("S1E1 audio = %+v", first)
	}
	if first.AudioFallback {
		t.Error("S1E1 got the requested voiceover — not a substitute")
	}

	second, ok := byKey["S1E2"]
	if !ok || !second.AudioFallback {
		t.Errorf("S1E2 should be marked as substituted: %+v", second)
	}
}
