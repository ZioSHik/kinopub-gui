package gui

import (
	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteEpisodeRejectsSymlinkedParent(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "keep.mkv")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := writeSeries(t, root, map[string]domain.CompletedRec{"S1E1": {Path: "placeholder.mkv"}})
	if err := os.Remove(filepath.Join(dir, "placeholder.mkv")); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"series":"42","completed":{"S1E1":{"path":"link/keep.mkv"}}}`)
	if err := os.WriteFile(filepath.Join(dir, stateFileName), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	if err := deleteLibraryEpisode(dir, "S1E1", []string{root}); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep" {
		t.Fatalf("outside file changed: %q %v", data, err)
	}
}

func TestDeleteSeriesRejectsSymlinkOutsideRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	dir := writeSeries(t, outside, map[string]domain.CompletedRec{"S1E1": {Path: "ep.mkv"}})
	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip(err)
	}
	if err := deleteLibrarySeries(link, []string{root}); err == nil {
		t.Fatal("outside symlink accepted")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("outside directory removed", err)
	}
}

func TestLibraryVersionsAreCountedAndDeletedTogether(t *testing.T) {
	root := t.TempDir()
	dir := writeSeries(t, root, map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1, Path: "a.mkv", Bytes: 1, Resolution: "1280x720", Versions: []domain.CompletedRec{{Season: 1, Episode: 1, Path: "b.mkv", Bytes: 2, Resolution: "1920x1080"}}}})
	if err := os.WriteFile(filepath.Join(dir, "b.mkv"), []byte("bb"), 0600); err != nil {
		t.Fatal(err)
	}
	series, ok := readLibraryState(filepath.Join(dir, stateFileName))
	if !ok || series.Count != 1 || series.TotalBytes != 3 || len(series.Episodes[0].Versions) != 1 {
		t.Fatalf("series=%+v", series)
	}
	if err := deleteLibraryEpisode(dir, "S1E1", []string{root}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.mkv", "b.mkv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("version %s remains: %v", name, err)
		}
	}
}

func TestSettingsSaveFailureKeepsMemoryAndOwnsSlices(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &settingsStore{path: filepath.Join(blocker, "settings.json"), cur: defaultSettings()}
	old := s.get()
	incoming := old
	incoming.OutputPath = "changed"
	if _, err := s.save(incoming); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if s.get().OutputPath != old.OutputPath {
		t.Fatal("failed save changed active settings")
	}
	s.path = filepath.Join(root, "settings.json")
	incoming.LibraryDirs = []string{"first"}
	saved, err := s.save(incoming)
	if err != nil {
		t.Fatal(err)
	}
	incoming.LibraryDirs[0] = "changed"
	saved.LibraryDirs[0] = "changed again"
	got := s.get()
	got.LibraryDirs[0] = "third mutation"
	if s.get().LibraryDirs[0] != "first" {
		t.Fatal("settings expose internal slice")
	}
}

func TestCanonicalOutputDetectsAliases(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip(err)
	}
	if canonicalOutput(filepath.Join(real, "future")) != canonicalOutput(filepath.Join(alias, "future")) {
		t.Fatal("aliased new output directories differ")
	}
}

func TestDevVersionCannotSelfUpdate(t *testing.T) {
	if !(&updateChecker{current: "v1.1.2-dev"}).isDevBuild() {
		t.Fatal("dev build treated as release")
	}
	if (&updateChecker{current: "v1.1.2"}).isDevBuild() {
		t.Fatal("release treated as dev")
	}
}
