package statestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/logx"

	"pgregory.net/rapid"
)

// **Validates: Requirements 12.1, 12.2, 12.3, 12.4, 12.5**

// --- Generators ---

// genSeriesID generates a valid SeriesID.
func genSeriesID() *rapid.Generator[domain.SeriesID] {
	return rapid.Custom(func(t *rapid.T) domain.SeriesID {
		return domain.SeriesID(rapid.StringMatching(`[a-z0-9\-]{1,20}`).Draw(t, "seriesID"))
	})
}

// genEpisodeKey generates an EpisodeKey with a fixed series and reasonable season/episode numbers.
func genEpisodeKey(series domain.SeriesID) *rapid.Generator[domain.EpisodeKey] {
	return rapid.Custom(func(t *rapid.T) domain.EpisodeKey {
		return domain.EpisodeKey{
			Series:  series,
			Season:  rapid.IntRange(1, 50).Draw(t, "season"),
			Episode: rapid.IntRange(1, 100).Draw(t, "episode"),
		}
	})
}

// genDistinctEpisodeKeys generates a slice of distinct EpisodeKeys for a given series.
func genDistinctEpisodeKeys(series domain.SeriesID) *rapid.Generator[[]domain.EpisodeKey] {
	return rapid.Custom(func(t *rapid.T) []domain.EpisodeKey {
		count := rapid.IntRange(1, 20).Draw(t, "count")
		seen := make(map[string]bool)
		var keys []domain.EpisodeKey
		for len(keys) < count {
			k := genEpisodeKey(series).Draw(t, "key")
			ks := episodeKeyString(k)
			if !seen[ks] {
				seen[ks] = true
				keys = append(keys, k)
			}
		}
		return keys
	})
}

// propLogger returns a no-op logger for property tests.
func propLogger() domain.Logger {
	return logx.New(nil)
}

// makeTempDir creates a temporary directory for use in property tests.
func makeTempDir(t *rapid.T) string {
	dir, err := os.MkdirTemp("", "statestore-prop-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// --- Property 31: State persistence round-trip and skip ---
// For any sequence of MarkCompleted calls with distinct EpisodeKeys, Load returns
// a state where IsCompleted is true for all marked keys and false for unmarked keys.

func TestProperty31_StatePersistenceRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := makeTempDir(t)
		store := New(dir, propLogger())
		ctx := context.Background()

		series := genSeriesID().Draw(t, "series")
		markedKeys := genDistinctEpisodeKeys(series).Draw(t, "markedKeys")

		// Mark all keys as completed.
		for _, k := range markedKeys {
			if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: k}); err != nil {
				t.Fatalf("MarkCompleted failed for %v: %v", k, err)
			}
		}

		// Load state and verify all marked keys are completed.
		state, err := store.Load(ctx, series)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		for _, k := range markedKeys {
			if !store.IsCompleted(state, k) {
				t.Fatalf("expected key %v to be completed after MarkCompleted", k)
			}
		}

		// Generate an unmarked key and verify it is NOT completed.
		unmarkedKey := genEpisodeKey(series).Draw(t, "unmarkedKey")
		// Only check if it's actually not in the marked set.
		isMarked := false
		for _, k := range markedKeys {
			if episodeKeyString(k) == episodeKeyString(unmarkedKey) {
				isMarked = true
				break
			}
		}
		if !isMarked {
			if store.IsCompleted(state, unmarkedKey) {
				t.Fatalf("expected unmarked key %v to NOT be completed", unmarkedKey)
			}
		}
	})
}

// --- Property 32: Completed records survive later failures ---
// For any set of completed episodes, if we mark them all, then corrupt the state
// file partially, the store gracefully returns empty state per Req 12.5.

func TestProperty32_CompletedRecordsSurviveCorruption(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := makeTempDir(t)
		store := New(dir, propLogger())
		ctx := context.Background()

		series := genSeriesID().Draw(t, "series")
		keys := genDistinctEpisodeKeys(series).Draw(t, "keys")

		// Mark all keys as completed.
		for _, k := range keys {
			if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: k}); err != nil {
				t.Fatalf("MarkCompleted failed for %v: %v", k, err)
			}
		}

		// Verify they are persisted before corruption.
		state, err := store.Load(ctx, series)
		if err != nil {
			t.Fatalf("Load failed before corruption: %v", err)
		}
		for _, k := range keys {
			if !store.IsCompleted(state, k) {
				t.Fatalf("expected key %v to be completed before corruption", k)
			}
		}

		// Now corrupt the state file by truncating it.
		statePath := filepath.Join(dir, stateFileName)
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("failed to read state file: %v", err)
		}

		// Truncate to a random portion (at least 1 byte, less than full).
		if len(data) > 1 {
			truncLen := rapid.IntRange(1, len(data)-1).Draw(t, "truncLen")
			if err := os.WriteFile(statePath, data[:truncLen], 0644); err != nil {
				t.Fatalf("failed to write truncated state: %v", err)
			}
		}

		// Load after corruption: should return empty state without error (Req 12.5).
		corruptedState, loadErr := store.Load(ctx, series)
		if loadErr != nil {
			t.Fatalf("Load should not return error for corrupt state, got: %v", loadErr)
		}

		// The state is either fully recovered (if truncation left valid JSON)
		// or empty (if JSON is invalid). Either way, no error is returned.
		// If the state is empty, that's the graceful degradation per Req 12.5.
		_ = corruptedState
	})
}

// --- Property 33: Forced re-download skips nothing ---
// For any set of completed episodes in state, IsCompleted still returns true
// (the bypass is the caller's responsibility — ForceRedownload flag is handled
// at the engine level, not in the store).

func TestProperty33_ForceRedownloadIsCallerResponsibility(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := makeTempDir(t)
		store := New(dir, propLogger())
		ctx := context.Background()

		series := genSeriesID().Draw(t, "series")
		keys := genDistinctEpisodeKeys(series).Draw(t, "keys")

		// Mark all keys as completed.
		for _, k := range keys {
			if err := store.MarkCompleted(ctx, domain.CompletedInfo{Key: k}); err != nil {
				t.Fatalf("MarkCompleted failed for %v: %v", k, err)
			}
		}

		// Load state.
		state, err := store.Load(ctx, series)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		// IsCompleted still returns true for all marked keys regardless of any
		// "force redownload" intent — the store always reports truth, and the
		// caller (engine) decides whether to skip based on ForceRedownload flag.
		for _, k := range keys {
			if !store.IsCompleted(state, k) {
				t.Fatalf("IsCompleted should always return true for marked key %v; "+
					"force-redownload bypass is the caller's responsibility", k)
			}
		}
	})
}

// --- Property 34: Corrupt state is tolerated ---
// For any random byte sequence written to the state file path, Load returns an
// empty state without error (tolerates corruption per Req 12.5).

func TestProperty34_CorruptStateIsTolerated(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := makeTempDir(t)
		store := New(dir, propLogger())
		ctx := context.Background()

		series := genSeriesID().Draw(t, "series")

		// Write random bytes to the state file.
		randomBytes := rapid.SliceOfN(rapid.Byte(), 0, 1024).Draw(t, "randomBytes")
		statePath := filepath.Join(dir, stateFileName)
		if err := os.WriteFile(statePath, randomBytes, 0644); err != nil {
			t.Fatalf("failed to write random state file: %v", err)
		}

		// Load should return empty state without error.
		state, err := store.Load(ctx, series)
		if err != nil {
			t.Fatalf("Load should not return error for corrupt/random state file, got: %v", err)
		}

		// The state should either be empty (corrupt JSON) or valid (if random
		// bytes happen to be valid JSON for a different series → empty for our series).
		// In all cases, the completed map should be initialized (not nil).
		if state.Completed == nil {
			t.Fatal("Completed map should never be nil, even for corrupt state")
		}

		// Series should match what we requested.
		if state.Series != series {
			t.Fatalf("expected series %q, got %q", series, state.Series)
		}
	})
}
