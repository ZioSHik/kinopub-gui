package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// writeSeries lays down a series folder with a state file and one dummy media
// file per completed episode, returning the series directory.
func writeSeries(t *testing.T, root string, completed map[string]domain.CompletedRec) string {
	t.Helper()
	dir := filepath.Join(root, "Test Series")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rec := range completed {
		if rec.Path == "" {
			continue
		}
		p := rec.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := domain.DownloadState{Series: "42", Completed: completed}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, stateFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readCompleted(t *testing.T, dir string) map[string]domain.CompletedRec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	var st domain.DownloadState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st.Completed
}

func TestDeleteLibraryEpisode_RemovesFileAndRecord(t *testing.T) {
	root := t.TempDir()
	dir := writeSeries(t, root, map[string]domain.CompletedRec{
		"S1E1": {Season: 1, Episode: 1, Path: "s1e1.mkv", Bytes: 1},
		"S1E2": {Season: 1, Episode: 2, Path: "s1e2.mkv", Bytes: 1},
	})

	if err := deleteLibraryEpisode(dir, "S1E1", []string{root}); err != nil {
		t.Fatalf("deleteLibraryEpisode: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "s1e1.mkv")); !os.IsNotExist(err) {
		t.Errorf("deleted episode file still present (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "s1e2.mkv")); err != nil {
		t.Errorf("sibling episode file was removed: %v", err)
	}
	got := readCompleted(t, dir)
	if _, ok := got["S1E1"]; ok {
		t.Error("deleted episode still recorded in state")
	}
	if _, ok := got["S1E2"]; !ok {
		t.Error("sibling episode record was dropped")
	}
}

func TestDeleteLibraryEpisode_LastEpisodeRemovesFolder(t *testing.T) {
	root := t.TempDir()
	dir := writeSeries(t, root, map[string]domain.CompletedRec{
		"S1E1": {Season: 1, Episode: 1, Path: "s1e1.mkv", Bytes: 1},
	})

	if err := deleteLibraryEpisode(dir, "S1E1", []string{root}); err != nil {
		t.Fatalf("deleteLibraryEpisode: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("series folder should be gone after last episode (err=%v)", err)
	}
}

func TestDeleteLibraryEpisode_Rejects(t *testing.T) {
	root := t.TempDir()
	dir := writeSeries(t, root, map[string]domain.CompletedRec{
		"S1E1": {Season: 1, Episode: 1, Path: "s1e1.mkv", Bytes: 1},
	})

	// Folder outside the configured roots is rejected.
	if err := deleteLibraryEpisode(dir, "S1E1", []string{t.TempDir()}); err == nil {
		t.Error("expected rejection for folder outside configured roots")
	}
	// Unknown episode key is rejected, leaving state untouched.
	if err := deleteLibraryEpisode(dir, "S9E9", []string{root}); err == nil {
		t.Error("expected error for unknown episode key")
	}
	if _, ok := readCompleted(t, dir)["S1E1"]; !ok {
		t.Error("state was mutated despite rejected delete")
	}
}

// A tampered state file whose Path escapes the series folder must not delete
// files elsewhere on disk.
func TestDeleteLibraryEpisode_RejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "victim.mkv")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := writeSeries(t, root, map[string]domain.CompletedRec{
		"S1E1": {Season: 1, Episode: 1, Path: "../victim.mkv", Bytes: 1},
	})

	if err := deleteLibraryEpisode(dir, "S1E1", []string{root}); err == nil {
		t.Error("expected path-traversal delete to be rejected")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("file outside the series folder was deleted: %v", err)
	}
}
