package statestore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestIsCompletedRequiresAvailableFile(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, testLogger())
	store.SetSeriesDir(dir)
	key := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
	for _, tc := range []struct {
		name, path string
		data       []byte
		want       bool
	}{
		{"absolute file", filepath.Join(dir, "absolute.mkv"), []byte("video"), true},
		{"relative file", "relative.mkv", []byte("video"), true},
		{"deleted file", "deleted.mkv", nil, false},
		{"empty file", "empty.mkv", []byte{}, false},
		{"empty path", "", nil, false},
		{"directory", dir, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if tc.data != nil {
				if err := os.WriteFile(path, tc.data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			state := domain.DownloadState{Series: "42", Completed: map[string]domain.CompletedRec{
				"S1E1": {Season: 1, Episode: 1, Path: tc.path, Bytes: 100},
			}}
			if got := store.IsCompleted(state, key); got != tc.want {
				t.Fatalf("IsCompleted=%v, want %v", got, tc.want)
			}
			if tc.want {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if store.IsCompleted(state, key) {
					t.Fatal("a file removed after loading state must be downloaded again")
				}
			}
			if len(state.Completed) != 1 {
				t.Fatal("checking presence must preserve the completion history")
			}
		})
	}
}

func TestLoadResolvesRelativeCompletedFiles(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "series state"
		if legacy {
			name = "legacy root state"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			seriesDir := filepath.Join(root, "Show")
			stateDir := seriesDir
			if legacy {
				stateDir = root
			}
			if err := os.MkdirAll(stateDir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(stateDir, "episode.mkv")
			if err := os.WriteFile(path, []byte("video"), 0600); err != nil {
				t.Fatal(err)
			}
			original := domain.DownloadState{Series: "42", Completed: map[string]domain.CompletedRec{
				"S1E1": {Season: 1, Episode: 1, Path: "episode.mkv"},
			}}
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateDir, stateFileName), data, 0600); err != nil {
				t.Fatal(err)
			}
			store := New(root, testLogger())
			store.SetSeriesDir(seriesDir)
			state, err := store.Load(context.Background(), "42")
			if err != nil {
				t.Fatal(err)
			}
			key := domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}
			if state.Completed["S1E1"].Path != path || !store.IsCompleted(state, key) {
				t.Fatalf("relative file was resolved against the wrong directory: %+v", state.Completed)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if store.IsCompleted(state, key) {
				t.Fatal("a missing relative file must be downloaded again")
			}
		})
	}
}
