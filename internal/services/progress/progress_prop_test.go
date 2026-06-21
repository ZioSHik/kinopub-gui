package progress

import (
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/logx"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// genTotalEpisodes generates a total episode count in [1,100].
func genTotalEpisodes() *rapid.Generator[int] {
	return rapid.IntRange(1, 100)
}

// genCompletedCount generates a completed count in [0, total].
func genCompletedCount(total int) *rapid.Generator[int] {
	return rapid.IntRange(0, total)
}

// genAnyInt generates any integer (including negatives and large values) for
// testing clampPercent robustness.
func genAnyInt() *rapid.Generator[int] {
	return rapid.IntRange(-1000, 1000)
}

// genSeriesPlan generates a valid SeriesPlan with 1-5 seasons, each having
// 1-20 episodes, and Total equal to the sum of all season episode counts.
func genSeriesPlan() *rapid.Generator[domain.SeriesPlan] {
	return rapid.Custom[domain.SeriesPlan](func(t *rapid.T) domain.SeriesPlan {
		numSeasons := rapid.IntRange(1, 5).Draw(t, "numSeasons")
		seasons := make(map[int]int, numSeasons)
		total := 0
		for i := 1; i <= numSeasons; i++ {
			count := rapid.IntRange(1, 20).Draw(t, "seasonEpCount")
			seasons[i] = count
			total += count
		}
		return domain.SeriesPlan{
			Total:   total,
			Seasons: seasons,
		}
	})
}

// genEpisodeCompletedSequence generates a sequence of EpisodeCompleted events
// for a given plan. It returns a slice of distinct EpisodeKeys representing
// the order in which episodes complete.
func genEpisodeCompletedSequence(plan domain.SeriesPlan) *rapid.Generator[[]domain.EpisodeKey] {
	return rapid.Custom[[]domain.EpisodeKey](func(t *rapid.T) []domain.EpisodeKey {
		// Build all possible episode keys from the plan.
		var allKeys []domain.EpisodeKey
		for season, count := range plan.Seasons {
			for ep := 1; ep <= count; ep++ {
				allKeys = append(allKeys, domain.EpisodeKey{
					Series:  "test-series",
					Season:  season,
					Episode: ep,
				})
			}
		}

		// Pick how many episodes to complete: [1, total].
		numToComplete := rapid.IntRange(1, len(allKeys)).Draw(t, "numToComplete")

		// Select distinct keys by drawing indices from the remaining pool.
		remaining := make([]domain.EpisodeKey, len(allKeys))
		copy(remaining, allKeys)
		result := make([]domain.EpisodeKey, 0, numToComplete)
		for i := 0; i < numToComplete; i++ {
			idx := rapid.IntRange(0, len(remaining)-1).Draw(t, "idx")
			result = append(result, remaining[idx])
			// Remove selected element by swapping with last.
			remaining[idx] = remaining[len(remaining)-1]
			remaining = remaining[:len(remaining)-1]
		}
		return result
	})
}

// ---------------------------------------------------------------------------
// captureHandler records all log records for inspection.
// ---------------------------------------------------------------------------

type captureHandler struct {
	records []logx.Record
}

func (h *captureHandler) Handle(rec logx.Record) {
	h.records = append(h.records, rec)
}

// ---------------------------------------------------------------------------
// Property 26: Progress percentages are floored and bounded
// ---------------------------------------------------------------------------

// **Validates: Requirements 10.1, 10.2, 10.3, 10.4**
//
// For any total episodes [1,100] and any completed count [0,total],
// computePercent returns a value in [0,100] that equals floor(100*completed/total).
// Also verify clampPercent always returns [0,100] for any input.
func TestProperty26_ProgressPercentagesFlooredAndBounded(t *testing.T) {
	t.Run("computePercent", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			total := genTotalEpisodes().Draw(t, "total")
			completed := genCompletedCount(total).Draw(t, "completed")

			got := computePercent(completed, total)

			// Must be in [0, 100].
			if got < 0 || got > 100 {
				t.Fatalf("computePercent(%d, %d) = %d, want [0,100]", completed, total, got)
			}

			// Must equal floor(100 * completed / total).
			expected := (100 * completed) / total
			if got != expected {
				t.Fatalf("computePercent(%d, %d) = %d, want floor(%d) = %d",
					completed, total, got, 100*completed, expected)
			}
		})
	})

	t.Run("computePercent_zeroTotal", func(t *testing.T) {
		// When total is 0 or negative, computePercent should return 0.
		rapid.Check(t, func(t *rapid.T) {
			total := rapid.IntRange(-100, 0).Draw(t, "total")
			completed := genAnyInt().Draw(t, "completed")

			got := computePercent(completed, total)
			if got != 0 {
				t.Fatalf("computePercent(%d, %d) = %d, want 0 for non-positive total",
					completed, total, got)
			}
		})
	})

	t.Run("clampPercent", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			input := genAnyInt().Draw(t, "input")

			got := clampPercent(input)

			// Must always be in [0, 100].
			if got < 0 || got > 100 {
				t.Fatalf("clampPercent(%d) = %d, want [0,100]", input, got)
			}

			// Verify clamping logic.
			var expected int
			switch {
			case input < 0:
				expected = 0
			case input > 100:
				expected = 100
			default:
				expected = input
			}
			if got != expected {
				t.Fatalf("clampPercent(%d) = %d, want %d", input, got, expected)
			}
		})
	})
}

// ---------------------------------------------------------------------------
// Property 27: Non-interactive progress emits records with current percentages
// ---------------------------------------------------------------------------

// **Validates: Requirements 10.1, 10.2, 10.4, 10.6**
//
// For any SeriesPlan and sequence of EpisodeCompleted events, the LogReporter
// emits log records that include series_percent and season_percent fields with
// values in [0,100].
func TestProperty27_NonInteractiveProgressEmitsPercentages(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		plan := genSeriesPlan().Draw(t, "plan")
		completedKeys := genEpisodeCompletedSequence(plan).Draw(t, "completedKeys")

		// Create a LogReporter with a capturing logger.
		h := &captureHandler{}
		logger := logx.New([]logx.Handler{h})
		reporter := NewLog(logger)

		// Start the reporter with the plan.
		reporter.Start(plan)

		// Clear the "started" record.
		h.records = nil

		// Feed EpisodeCompleted events.
		for _, key := range completedKeys {
			reporter.EpisodeCompleted(key)
		}

		// Verify that each EpisodeCompleted call emitted a record with
		// series_percent and season_percent fields in [0,100].
		if len(h.records) != len(completedKeys) {
			t.Fatalf("expected %d records, got %d", len(completedKeys), len(h.records))
		}

		for i, rec := range h.records {
			seriesPct := findIntField(rec.Fields, "series_percent")
			seasonPct := findIntField(rec.Fields, "season_percent")

			if seriesPct == nil {
				t.Fatalf("record %d: missing series_percent field", i)
			}
			if seasonPct == nil {
				t.Fatalf("record %d: missing season_percent field", i)
			}

			if *seriesPct < 0 || *seriesPct > 100 {
				t.Fatalf("record %d: series_percent=%d, want [0,100]", i, *seriesPct)
			}
			if *seasonPct < 0 || *seasonPct > 100 {
				t.Fatalf("record %d: season_percent=%d, want [0,100]", i, *seasonPct)
			}
		}

		// Verify final series_percent matches expected value.
		// After all completedKeys are processed, the series percent should be
		// floor(100 * len(completedKeys) / plan.Total).
		if len(h.records) > 0 {
			lastRec := h.records[len(h.records)-1]
			finalSeriesPct := findIntField(lastRec.Fields, "series_percent")
			expectedFinal := computePercent(len(completedKeys), plan.Total)
			if *finalSeriesPct != expectedFinal {
				t.Fatalf("final series_percent=%d, want %d (completed=%d, total=%d)",
					*finalSeriesPct, expectedFinal, len(completedKeys), plan.Total)
			}
		}
	})
}

// findIntField searches for a field by key and returns its int value, or nil
// if not found.
func findIntField(fields []domain.Field, key string) *int {
	for _, f := range fields {
		if f.Key == key {
			switch v := f.Value.(type) {
			case int:
				return &v
			}
		}
	}
	return nil
}
