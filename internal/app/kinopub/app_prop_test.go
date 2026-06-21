package kinopub

import (
	"errors"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// **Validates: Requirements 16.5**

// Property 44: Missing dependency is detected before any work
//
// For any single nil dependency field in the Dependencies struct (chosen
// randomly from the 10 fields), New() returns an error that wraps
// ErrMissingDependency.

func TestProperty44_MissingDependencyDetectedBeforeWork(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Start with a fully valid Dependencies struct (all fields non-nil)
		deps := validDeps()

		// Randomly select one of the 10 dependency fields to set to nil
		fieldIndex := rapid.IntRange(0, 9).Draw(t, "fieldIndex")

		switch fieldIndex {
		case 0:
			deps.Logger = nil
		case 1:
			deps.InputResolver = nil
		case 2:
			deps.FeedParser = nil
		case 3:
			deps.MediaResolver = nil
		case 4:
			deps.Scheduler = nil
		case 5:
			deps.Downloader = nil
		case 6:
			deps.ProxyProvider = nil
		case 7:
			deps.ProgressReporter = nil
		case 8:
			deps.StateStore = nil
		case 9:
			deps.OutputLayout = nil
		}

		// Call New() and verify it returns ErrMissingDependency
		app, err := New(deps)
		if err == nil {
			t.Fatalf("expected error for nil field index %d, got nil error and app=%v", fieldIndex, app)
		}
		if !errors.Is(err, domain.ErrMissingDependency) {
			t.Fatalf("expected ErrMissingDependency for nil field index %d, got: %v", fieldIndex, err)
		}
		if app != nil {
			t.Fatalf("expected nil App when dependency is missing (field index %d), got non-nil", fieldIndex)
		}
	})
}
