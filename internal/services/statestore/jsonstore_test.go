package statestore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/logx"
)

// testLogger returns a no-op logger suitable for tests.
func testLogger() domain.Logger {
	return logx.New(nil)
}

func TestNew(t *testing.T) {
	store := New("/tmp/test", testLogger())
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, testLogger())

	state, err := store.Load(context.Background(), "series-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Series != "series-1" {
		t.Errorf("expected series 'series-1', got %q", state.Series)
	}
	if len(state.Completed) != 0 {
		t.Errorf("expected empty completed map, got %d entries", len(state.Completed))
	}
}

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	// Write garbage to the state file.
	err := os.WriteFile(filepath.Join(dir, stateFileName), []byte("not json{{{"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	store := New(dir, testLogger())
	state, loadErr := store.Load(context.Background(), "series-1")
	if loadErr != nil {
		t.Fatalf("expected no error for corrupt file, got: %v", loadErr)
	}
	if state.Series != "series-1" {
		t.Errorf("expected series 'series-1', got %q", state.Series)
	}
	if len(state.Completed) != 0 {
		t.Errorf("expected empty completed map, got %d entries", len(state.Completed))
	}
}

func TestMarkCompletedAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, testLogger())

	key := domain.EpisodeKey{Series: "series-1", Season: 2, Episode: 5}
	info := domain.CompletedInfo{
		Key:      key,
		Path:     "/tmp/test/S02E05.mkv",
		Bytes:    123456,
		Title:    "Test Episode",
		Quality:  "1080p",
		PageLink: "https://example.com/s02e05",
		MediaURL: "https://cdn.example.com/video.mp4",
	}
	err := store.MarkCompleted(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the file was created.
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	var state domain.DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}

	if state.Series != "series-1" {
		t.Errorf("expected series 'series-1', got %q", state.Series)
	}

	rec, ok := state.Completed["S2E5"]
	if !ok {
		t.Fatal("expected S2E5 in completed map")
	}
	if rec.Season != 2 || rec.Episode != 5 {
		t.Errorf("expected season=2, episode=5, got season=%d, episode=%d", rec.Season, rec.Episode)
	}
	if rec.Path != "/tmp/test/S02E05.mkv" {
		t.Errorf("expected path '/tmp/test/S02E05.mkv', got %q", rec.Path)
	}
	if rec.Bytes != 123456 {
		t.Errorf("expected bytes 123456, got %d", rec.Bytes)
	}
	if rec.Title != "Test Episode" {
		t.Errorf("expected title 'Test Episode', got %q", rec.Title)
	}

	// Load and verify.
	loaded, err := store.Load(context.Background(), "series-1")
	if err != nil {
		t.Fatalf("unexpected error on load: %v", err)
	}
	if _, ok := loaded.Completed["S2E5"]; !ok {
		t.Error("expected S2E5 in loaded state")
	}
}

func TestIsCompleted(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, testLogger())

	key := domain.EpisodeKey{Series: "series-1", Season: 1, Episode: 3}
	_ = store.MarkCompleted(context.Background(), domain.CompletedInfo{Key: key, Path: "/tmp/ep.mkv", Bytes: 100})

	state, _ := store.Load(context.Background(), "series-1")

	if !store.IsCompleted(state, key) {
		t.Error("expected episode to be completed")
	}

	otherKey := domain.EpisodeKey{Series: "series-1", Season: 1, Episode: 4}
	if store.IsCompleted(state, otherKey) {
		t.Error("expected episode NOT to be completed")
	}
}

func TestMarkCompletedMultiple(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, testLogger())

	keys := []domain.EpisodeKey{
		{Series: "s1", Season: 1, Episode: 1},
		{Series: "s1", Season: 1, Episode: 2},
		{Series: "s1", Season: 2, Episode: 1},
	}

	for _, k := range keys {
		if err := store.MarkCompleted(context.Background(), domain.CompletedInfo{Key: k, Path: "/tmp/ep.mkv", Bytes: 100}); err != nil {
			t.Fatalf("unexpected error marking %v: %v", k, err)
		}
	}

	state, err := store.Load(context.Background(), "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(state.Completed) != 3 {
		t.Errorf("expected 3 completed entries, got %d", len(state.Completed))
	}

	for _, k := range keys {
		if !store.IsCompleted(state, k) {
			t.Errorf("expected %v to be completed", k)
		}
	}
}

func TestLoadDifferentSeries(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, testLogger())

	// Mark an episode for series "alpha".
	key := domain.EpisodeKey{Series: "alpha", Season: 1, Episode: 1}
	_ = store.MarkCompleted(context.Background(), domain.CompletedInfo{Key: key, Path: "/tmp/ep.mkv", Bytes: 100})

	// Load for a different series should return empty.
	state, err := store.Load(context.Background(), "beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.Completed) != 0 {
		t.Errorf("expected empty state for different series, got %d entries", len(state.Completed))
	}
}

func TestEpisodeKeyString(t *testing.T) {
	tests := []struct {
		key  domain.EpisodeKey
		want string
	}{
		{domain.EpisodeKey{Season: 1, Episode: 1}, "S1E1"},
		{domain.EpisodeKey{Season: 4, Episode: 8}, "S4E8"},
		{domain.EpisodeKey{Season: 10, Episode: 22}, "S10E22"},
	}

	for _, tt := range tests {
		got := episodeKeyString(tt.key)
		if got != tt.want {
			t.Errorf("episodeKeyString(%v) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
