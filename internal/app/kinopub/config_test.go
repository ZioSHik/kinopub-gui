package kinopub

import (
	"errors"
	"testing"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func validConfig() *domain.RunConfig {
	return &domain.RunConfig{
		MaxConcurrency: 2,
		MaxRetries:     5,
		MinIntervalMS:  1000,
		Verbosity:      domain.VerbosityNormal,
		Container:      domain.ContainerMKV,
	}
}

// --- ValidateConfig tests ---

func TestValidateConfig_ValidConfig(t *testing.T) {
	cfg := validConfig()
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateConfig_MaxConcurrency(t *testing.T) {
	tests := []struct {
		name  string
		value int
		valid bool
	}{
		{"zero", 0, false},
		{"one", 1, true},
		{"sixteen", 16, true},
		{"seventeen", 17, false},
		{"negative", -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.MaxConcurrency = tc.value
			err := ValidateConfig(cfg)
			if tc.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, domain.ErrInvalidFlag) {
					t.Errorf("expected ErrInvalidFlag, got %v", err)
				}
			}
		})
	}
}

func TestValidateConfig_MaxRetries(t *testing.T) {
	tests := []struct {
		name  string
		value int
		valid bool
	}{
		{"zero", 0, true},
		{"positive", 10, true},
		{"negative", -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.MaxRetries = tc.value
			err := ValidateConfig(cfg)
			if tc.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, domain.ErrInvalidFlag) {
					t.Errorf("expected ErrInvalidFlag, got %v", err)
				}
			}
		})
	}
}

func TestValidateConfig_MinIntervalMS(t *testing.T) {
	tests := []struct {
		name  string
		value int
		valid bool
	}{
		{"zero", 0, true},
		{"mid", 5000, true},
		{"max", 60000, true},
		{"over_max", 60001, false},
		{"negative", -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.MinIntervalMS = tc.value
			err := ValidateConfig(cfg)
			if tc.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, domain.ErrInvalidFlag) {
					t.Errorf("expected ErrInvalidFlag, got %v", err)
				}
			}
		})
	}
}

func TestValidateConfig_Verbosity(t *testing.T) {
	tests := []struct {
		name  string
		value domain.Verbosity
		valid bool
	}{
		{"quiet", domain.VerbosityQuiet, true},
		{"normal", domain.VerbosityNormal, true},
		{"verbose", domain.VerbosityVerbose, true},
		{"invalid", domain.Verbosity(99), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Verbosity = tc.value
			err := ValidateConfig(cfg)
			if tc.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, domain.ErrInvalidFlag) {
					t.Errorf("expected ErrInvalidFlag, got %v", err)
				}
			}
		})
	}
}

func TestValidateConfig_ProxyURL(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{"empty", "", true},
		{"http", "http://proxy.example.com:8080", true},
		{"https", "https://proxy.example.com:443", true},
		{"socks5", "socks5://proxy.example.com:1080", true},
		{"no_scheme", "proxy.example.com:8080", false},
		{"bad_scheme", "ftp://proxy.example.com:21", false},
		{"no_host", "http://", false},
		{"socks4", "socks4://proxy.example.com:1080", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.ProxyURL = tc.value
			err := ValidateConfig(cfg)
			if tc.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, domain.ErrInvalidFlag) {
					t.Errorf("expected ErrInvalidFlag, got %v", err)
				}
			}
		})
	}
}

func TestValidateConfig_Container(t *testing.T) {
	tests := []struct {
		name  string
		value domain.Container
		valid bool
	}{
		{"mkv", domain.ContainerMKV, true},
		{"mp4", domain.ContainerMP4, true},
		{"invalid", domain.Container(99), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Container = tc.value
			err := ValidateConfig(cfg)
			if tc.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, domain.ErrInvalidFlag) {
					t.Errorf("expected ErrInvalidFlag, got %v", err)
				}
			}
		})
	}
}

// --- ApplyDefaults tests ---

func TestApplyDefaults_FillsAllDefaults(t *testing.T) {
	cfg := &domain.RunConfig{}
	ApplyDefaults(cfg)

	if cfg.MaxConcurrency != 2 {
		t.Errorf("MaxConcurrency = %d, want 2", cfg.MaxConcurrency)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
	}
	if cfg.Verbosity != domain.VerbosityNormal {
		t.Errorf("Verbosity = %d, want VerbosityNormal", cfg.Verbosity)
	}
	if cfg.FFmpegPath != "ffmpeg" {
		t.Errorf("FFmpegPath = %q, want %q", cfg.FFmpegPath, "ffmpeg")
	}
	if cfg.Container != domain.ContainerMKV {
		t.Errorf("Container = %d, want ContainerMKV", cfg.Container)
	}
	if cfg.GracePeriod != 30*time.Second {
		t.Errorf("GracePeriod = %v, want 30s", cfg.GracePeriod)
	}
	if !cfg.SeasonSel.All {
		t.Error("SeasonSel.All = false, want true")
	}
	if !cfg.EpisodeSel.All {
		t.Error("EpisodeSel.All = false, want true")
	}
}

func TestApplyDefaults_DoesNotOverrideExisting(t *testing.T) {
	cfg := &domain.RunConfig{
		MaxConcurrency: 8,
		MaxRetries:     3,
		Verbosity:      domain.VerbosityVerbose,
		FFmpegPath:     "/usr/local/bin/ffmpeg",
		Container:      domain.ContainerMP4,
		GracePeriod:    10 * time.Second,
		SeasonSel:      domain.Selection{Values: map[int]bool{1: true}},
		EpisodeSel:     domain.Selection{Ranges: []domain.SelectionRange{{Lo: 1, Hi: 5}}},
	}
	ApplyDefaults(cfg)

	if cfg.MaxConcurrency != 8 {
		t.Errorf("MaxConcurrency = %d, want 8", cfg.MaxConcurrency)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.Verbosity != domain.VerbosityVerbose {
		t.Errorf("Verbosity = %d, want VerbosityVerbose", cfg.Verbosity)
	}
	if cfg.FFmpegPath != "/usr/local/bin/ffmpeg" {
		t.Errorf("FFmpegPath = %q, want /usr/local/bin/ffmpeg", cfg.FFmpegPath)
	}
	if cfg.Container != domain.ContainerMP4 {
		t.Errorf("Container = %d, want ContainerMP4", cfg.Container)
	}
	if cfg.GracePeriod != 10*time.Second {
		t.Errorf("GracePeriod = %v, want 10s", cfg.GracePeriod)
	}
	if cfg.SeasonSel.All {
		t.Error("SeasonSel.All = true, want false (should not override)")
	}
	if cfg.EpisodeSel.All {
		t.Error("EpisodeSel.All = true, want false (should not override)")
	}
}

// --- ParseSelection tests ---

func TestParseSelection_Empty(t *testing.T) {
	sel, err := ParseSelection("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sel.All {
		t.Error("expected All=true for empty string")
	}
}

func TestParseSelection_Whitespace(t *testing.T) {
	sel, err := ParseSelection("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sel.All {
		t.Error("expected All=true for whitespace-only string")
	}
}

func TestParseSelection_SingleNumbers(t *testing.T) {
	sel, err := ParseSelection("1,3,5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.All {
		t.Error("expected All=false")
	}
	for _, n := range []int{1, 3, 5} {
		if !sel.Matches(n) {
			t.Errorf("expected %d to match", n)
		}
	}
	for _, n := range []int{0, 2, 4, 6} {
		if sel.Matches(n) {
			t.Errorf("expected %d to not match", n)
		}
	}
}

func TestParseSelection_Range(t *testing.T) {
	sel, err := ParseSelection("3-7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range []int{3, 4, 5, 6, 7} {
		if !sel.Matches(n) {
			t.Errorf("expected %d to match", n)
		}
	}
	for _, n := range []int{1, 2, 8, 9} {
		if sel.Matches(n) {
			t.Errorf("expected %d to not match", n)
		}
	}
}

func TestParseSelection_Mixed(t *testing.T) {
	sel, err := ParseSelection("1,3-5,8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range []int{1, 3, 4, 5, 8} {
		if !sel.Matches(n) {
			t.Errorf("expected %d to match", n)
		}
	}
	for _, n := range []int{0, 2, 6, 7, 9} {
		if sel.Matches(n) {
			t.Errorf("expected %d to not match", n)
		}
	}
}

func TestParseSelection_SpacesAroundParts(t *testing.T) {
	sel, err := ParseSelection(" 1 , 3 - 5 , 8 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range []int{1, 3, 4, 5, 8} {
		if !sel.Matches(n) {
			t.Errorf("expected %d to match", n)
		}
	}
}

func TestParseSelection_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"letters", "abc"},
		{"empty_element", "1,,3"},
		{"bad_range_start", "x-5"},
		{"bad_range_end", "1-y"},
		{"reversed_range", "5-3"},
		{"trailing_comma", "1,"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSelection(tc.input)
			if err == nil {
				t.Error("expected error, got nil")
			} else if !errors.Is(err, domain.ErrInvalidFlag) {
				t.Errorf("expected ErrInvalidFlag, got %v", err)
			}
		})
	}
}
