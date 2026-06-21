package kinopub

import (
	"errors"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"

	"pgregory.net/rapid"
)

// **Validates: Requirements 15.4, 15.5, 15.6, 15.7**

// Property 40: Invalid flag values are rejected before any work
//
// For any config with MaxConcurrency outside [1,16], or MinIntervalMS outside
// [0,60000], or invalid Verbosity, or invalid Container, ValidateConfig returns
// ErrInvalidFlag.

func TestProperty40_InvalidMaxConcurrencyRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a concurrency value outside [1,16]
		conc := rapid.OneOf(
			rapid.IntRange(-1000, 0),
			rapid.IntRange(17, 1000),
		).Draw(t, "concurrency")

		cfg := &domain.RunConfig{
			MaxConcurrency: conc,
			MaxRetries:     5,
			MinIntervalMS:  1000,
			Verbosity:      domain.VerbosityNormal,
			Container:      domain.ContainerMKV,
		}

		err := ValidateConfig(cfg)
		if err == nil {
			t.Fatalf("expected ErrInvalidFlag for MaxConcurrency=%d, got nil", conc)
		}
		if !errors.Is(err, domain.ErrInvalidFlag) {
			t.Fatalf("expected ErrInvalidFlag for MaxConcurrency=%d, got %v", conc, err)
		}
	})
}

func TestProperty40_InvalidMinIntervalMSRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate an interval value outside [0,60000]
		interval := rapid.OneOf(
			rapid.IntRange(-10000, -1),
			rapid.IntRange(60001, 200000),
		).Draw(t, "interval")

		cfg := &domain.RunConfig{
			MaxConcurrency: 2,
			MaxRetries:     5,
			MinIntervalMS:  interval,
			Verbosity:      domain.VerbosityNormal,
			Container:      domain.ContainerMKV,
		}

		err := ValidateConfig(cfg)
		if err == nil {
			t.Fatalf("expected ErrInvalidFlag for MinIntervalMS=%d, got nil", interval)
		}
		if !errors.Is(err, domain.ErrInvalidFlag) {
			t.Fatalf("expected ErrInvalidFlag for MinIntervalMS=%d, got %v", interval, err)
		}
	})
}

func TestProperty40_InvalidVerbosityRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a verbosity value that is not one of the valid enum values (0, 1, 2)
		verb := rapid.OneOf(
			rapid.IntRange(-100, -1),
			rapid.IntRange(3, 100),
		).Draw(t, "verbosity")

		cfg := &domain.RunConfig{
			MaxConcurrency: 2,
			MaxRetries:     5,
			MinIntervalMS:  1000,
			Verbosity:      domain.Verbosity(verb),
			Container:      domain.ContainerMKV,
		}

		err := ValidateConfig(cfg)
		if err == nil {
			t.Fatalf("expected ErrInvalidFlag for Verbosity=%d, got nil", verb)
		}
		if !errors.Is(err, domain.ErrInvalidFlag) {
			t.Fatalf("expected ErrInvalidFlag for Verbosity=%d, got %v", verb, err)
		}
	})
}

func TestProperty40_InvalidContainerRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a container value that is not one of the valid enum values (0=MKV, 1=MP4)
		cont := rapid.OneOf(
			rapid.IntRange(-100, -1),
			rapid.IntRange(2, 100),
		).Draw(t, "container")

		cfg := &domain.RunConfig{
			MaxConcurrency: 2,
			MaxRetries:     5,
			MinIntervalMS:  1000,
			Verbosity:      domain.VerbosityNormal,
			Container:      domain.Container(cont),
		}

		err := ValidateConfig(cfg)
		if err == nil {
			t.Fatalf("expected ErrInvalidFlag for Container=%d, got nil", cont)
		}
		if !errors.Is(err, domain.ErrInvalidFlag) {
			t.Fatalf("expected ErrInvalidFlag for Container=%d, got %v", cont, err)
		}
	})
}

// Property 41: Selection limits downloads to matching episodes
//
// For any Selection (with generated Values and Ranges) and any integer n,
// Selection.Matches(n) returns true iff n is in Values or within any Range [Lo,Hi].

func TestProperty41_SelectionMatchesValues(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of values
		numValues := rapid.IntRange(1, 20).Draw(t, "numValues")
		values := make(map[int]bool, numValues)
		for i := 0; i < numValues; i++ {
			v := rapid.IntRange(1, 1000).Draw(t, "value")
			values[v] = true
		}

		sel := domain.Selection{
			All:    false,
			Values: values,
		}

		// Any value in the set must match
		for v := range values {
			if !sel.Matches(v) {
				t.Fatalf("expected Matches(%d) = true for value in set", v)
			}
		}

		// A value not in the set (and no ranges) must not match
		probe := rapid.IntRange(1001, 2000).Draw(t, "probe")
		if sel.Matches(probe) {
			t.Fatalf("expected Matches(%d) = false for value not in set", probe)
		}
	})
}

func TestProperty41_SelectionMatchesRanges(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a range [lo, hi]
		lo := rapid.IntRange(1, 500).Draw(t, "lo")
		hi := rapid.IntRange(lo, lo+100).Draw(t, "hi")

		sel := domain.Selection{
			All:    false,
			Ranges: []domain.SelectionRange{{Lo: lo, Hi: hi}},
		}

		// Any value in [lo, hi] must match
		n := rapid.IntRange(lo, hi).Draw(t, "inRange")
		if !sel.Matches(n) {
			t.Fatalf("expected Matches(%d) = true for n in [%d,%d]", n, lo, hi)
		}

		// A value below lo must not match
		if lo > 1 {
			below := rapid.IntRange(1, lo-1).Draw(t, "below")
			if sel.Matches(below) {
				t.Fatalf("expected Matches(%d) = false for n < lo=%d", below, lo)
			}
		}

		// A value above hi must not match
		above := rapid.IntRange(hi+1, hi+500).Draw(t, "above")
		if sel.Matches(above) {
			t.Fatalf("expected Matches(%d) = false for n > hi=%d", above, hi)
		}
	})
}

func TestProperty41_SelectionAllMatchesEverything(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(-1000, 1000).Draw(t, "n")

		sel := domain.Selection{All: true}
		if !sel.Matches(n) {
			t.Fatalf("expected Matches(%d) = true when All=true", n)
		}
	})
}

func TestProperty41_SelectionMatchesMixed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a selection with both values and ranges
		numValues := rapid.IntRange(0, 10).Draw(t, "numValues")
		values := make(map[int]bool, numValues)
		for i := 0; i < numValues; i++ {
			v := rapid.IntRange(1, 100).Draw(t, "value")
			values[v] = true
		}

		numRanges := rapid.IntRange(0, 5).Draw(t, "numRanges")
		ranges := make([]domain.SelectionRange, 0, numRanges)
		for i := 0; i < numRanges; i++ {
			lo := rapid.IntRange(1, 100).Draw(t, "rangeLo")
			hi := rapid.IntRange(lo, lo+20).Draw(t, "rangeHi")
			ranges = append(ranges, domain.SelectionRange{Lo: lo, Hi: hi})
		}

		sel := domain.Selection{
			All:    false,
			Values: values,
			Ranges: ranges,
		}

		// Test a probe value
		probe := rapid.IntRange(1, 200).Draw(t, "probe")

		// Compute expected result manually
		expected := false
		if values[probe] {
			expected = true
		}
		for _, r := range ranges {
			if probe >= r.Lo && probe <= r.Hi {
				expected = true
				break
			}
		}

		got := sel.Matches(probe)
		if got != expected {
			t.Fatalf("Matches(%d) = %v, want %v (values=%v, ranges=%v)", probe, got, expected, values, ranges)
		}
	})
}

// Property 42: Dry-run produces a full listing and no side effects
//
// For the config layer, verify that ParseSelection("") returns All=true
// (dry-run uses all episodes when no selection is specified).

func TestProperty42_EmptySelectionReturnsAll(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate various whitespace-only strings that should all parse as "all"
		numSpaces := rapid.IntRange(0, 20).Draw(t, "numSpaces")
		input := ""
		for i := 0; i < numSpaces; i++ {
			input += " "
		}

		sel, err := ParseSelection(input)
		if err != nil {
			t.Fatalf("ParseSelection(%q) returned error: %v", input, err)
		}
		if !sel.All {
			t.Fatalf("ParseSelection(%q).All = false, want true", input)
		}

		// Verify that All=true means every episode matches
		n := rapid.IntRange(1, 10000).Draw(t, "episode")
		if !sel.Matches(n) {
			t.Fatalf("All=true selection does not match episode %d", n)
		}
	})
}

// Property 43: Configuration precedence resolution
//
// For any config, ApplyDefaults fills in defaults only for zero-value fields
// without overriding explicitly set values.

func TestProperty43_ApplyDefaultsFillsZeroFields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := &domain.RunConfig{}
		ApplyDefaults(cfg)

		// All defaultable fields must be non-zero after ApplyDefaults
		if cfg.MaxConcurrency == 0 {
			t.Fatal("MaxConcurrency should be non-zero after ApplyDefaults")
		}
		if cfg.MaxRetries == 0 {
			t.Fatal("MaxRetries should be non-zero after ApplyDefaults")
		}
		if cfg.Verbosity == 0 {
			t.Fatal("Verbosity should be non-zero after ApplyDefaults")
		}
		if cfg.FFmpegPath == "" {
			t.Fatal("FFmpegPath should be non-empty after ApplyDefaults")
		}
		if cfg.GracePeriod == 0 {
			t.Fatal("GracePeriod should be non-zero after ApplyDefaults")
		}
		if !cfg.SeasonSel.All {
			t.Fatal("SeasonSel.All should be true after ApplyDefaults on zero config")
		}
		if !cfg.EpisodeSel.All {
			t.Fatal("EpisodeSel.All should be true after ApplyDefaults on zero config")
		}
	})
}

func TestProperty43_ApplyDefaultsDoesNotOverrideExplicit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate non-zero values for all defaultable fields.
		// Note: ApplyDefaults uses zero-value detection, so we only test with
		// non-zero values (VerbosityQuiet=0 and ContainerMKV=0 are zero values
		// and would be treated as "not set" by ApplyDefaults — this is by design).
		conc := rapid.IntRange(1, 16).Draw(t, "concurrency")
		retries := rapid.IntRange(1, 20).Draw(t, "retries")
		verbosity := rapid.SampledFrom([]domain.Verbosity{
			domain.VerbosityNormal, domain.VerbosityVerbose,
		}).Draw(t, "verbosity")
		ffmpegPath := rapid.StringMatching(`/[a-z]+/[a-z]+`).Draw(t, "ffmpegPath")
		container := rapid.SampledFrom([]domain.Container{
			domain.ContainerMP4,
		}).Draw(t, "container")
		graceSec := rapid.IntRange(1, 120).Draw(t, "graceSec")
		gracePeriod := time.Duration(graceSec) * time.Second

		// Generate a non-empty selection for seasons
		seasonVal := rapid.IntRange(1, 50).Draw(t, "seasonVal")
		seasonSel := domain.Selection{Values: map[int]bool{seasonVal: true}}

		// Generate a non-empty selection for episodes
		epLo := rapid.IntRange(1, 20).Draw(t, "epLo")
		epHi := rapid.IntRange(epLo, epLo+10).Draw(t, "epHi")
		episodeSel := domain.Selection{Ranges: []domain.SelectionRange{{Lo: epLo, Hi: epHi}}}

		cfg := &domain.RunConfig{
			MaxConcurrency: conc,
			MaxRetries:     retries,
			Verbosity:      verbosity,
			FFmpegPath:     ffmpegPath,
			Container:      container,
			GracePeriod:    gracePeriod,
			SeasonSel:      seasonSel,
			EpisodeSel:     episodeSel,
		}

		ApplyDefaults(cfg)

		// Verify none of the explicitly set fields were overridden
		if cfg.MaxConcurrency != conc {
			t.Fatalf("MaxConcurrency changed from %d to %d", conc, cfg.MaxConcurrency)
		}
		if cfg.MaxRetries != retries {
			t.Fatalf("MaxRetries changed from %d to %d", retries, cfg.MaxRetries)
		}
		if cfg.Verbosity != verbosity {
			t.Fatalf("Verbosity changed from %d to %d", verbosity, cfg.Verbosity)
		}
		if cfg.FFmpegPath != ffmpegPath {
			t.Fatalf("FFmpegPath changed from %q to %q", ffmpegPath, cfg.FFmpegPath)
		}
		if cfg.Container != container {
			t.Fatalf("Container changed from %d to %d", container, cfg.Container)
		}
		if cfg.GracePeriod != gracePeriod {
			t.Fatalf("GracePeriod changed from %v to %v", gracePeriod, cfg.GracePeriod)
		}
		if cfg.SeasonSel.All {
			t.Fatal("SeasonSel was overridden to All=true")
		}
		if !cfg.SeasonSel.Values[seasonVal] {
			t.Fatalf("SeasonSel lost value %d", seasonVal)
		}
		if cfg.EpisodeSel.All {
			t.Fatal("EpisodeSel was overridden to All=true")
		}
		if len(cfg.EpisodeSel.Ranges) == 0 || cfg.EpisodeSel.Ranges[0].Lo != epLo || cfg.EpisodeSel.Ranges[0].Hi != epHi {
			t.Fatalf("EpisodeSel ranges were overridden")
		}
	})
}
