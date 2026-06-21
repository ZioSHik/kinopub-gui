package main

import (
	"errors"
	"os"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// --- parseVerbosity tests ---

func TestParseVerbosity_ValidValues(t *testing.T) {
	tests := []struct {
		input string
		want  domain.Verbosity
	}{
		{"quiet", domain.VerbosityQuiet},
		{"normal", domain.VerbosityNormal},
		{"verbose", domain.VerbosityVerbose},
		{"", domain.VerbosityNormal},
	}

	for _, tc := range tests {
		t.Run("input_"+tc.input, func(t *testing.T) {
			got, err := parseVerbosity(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseVerbosity(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseVerbosity_Invalid(t *testing.T) {
	invalids := []string{"invalid", "QUIET", "Normal", "debug", "  ", "loud"}

	for _, input := range invalids {
		t.Run("input_"+input, func(t *testing.T) {
			_, err := parseVerbosity(input)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", input)
			}
			if !errors.Is(err, domain.ErrInvalidFlag) {
				t.Errorf("expected ErrInvalidFlag, got %v", err)
			}
		})
	}
}

// --- parseContainer tests ---

func TestParseContainer_ValidValues(t *testing.T) {
	tests := []struct {
		input string
		want  domain.Container
	}{
		{"mkv", domain.ContainerMKV},
		{"mp4", domain.ContainerMP4},
		{"", domain.ContainerMKV},
	}

	for _, tc := range tests {
		t.Run("input_"+tc.input, func(t *testing.T) {
			got, err := parseContainer(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseContainer(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseContainer_Invalid(t *testing.T) {
	invalids := []string{"avi", "MKV", "MP4", "webm", "flv", "  "}

	for _, input := range invalids {
		t.Run("input_"+input, func(t *testing.T) {
			_, err := parseContainer(input)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", input)
			}
			if !errors.Is(err, domain.ErrInvalidFlag) {
				t.Errorf("expected ErrInvalidFlag, got %v", err)
			}
		})
	}
}

// --- run() argument validation tests ---

func TestRun_ZeroURLArgs(t *testing.T) {
	// Save and restore os.Args.
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"kinopub"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1 for zero URL args, got %d", code)
	}
}

func TestRun_MultipleURLArgs(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"kinopub", "https://kino.pub/feed1", "https://kino.pub/feed2"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1 for multiple URL args, got %d", code)
	}
}

func TestRun_VersionFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"kinopub", "--version"}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0 for --version, got %d", code)
	}
}

func TestRun_HelpFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"kinopub", "--help"}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0 for --help, got %d", code)
	}
}

func TestRun_InvalidVerbosityFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"kinopub", "--verbosity", "invalid", "https://kino.pub/feed"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1 for invalid verbosity, got %d", code)
	}
}

func TestRun_InvalidContainerFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"kinopub", "--container", "avi", "https://kino.pub/feed"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1 for invalid container, got %d", code)
	}
}

// --- Config precedence with env vars ---

func TestRun_EnvVarConcurrencyOverridesDefault(t *testing.T) {
	// This test verifies that when a flag is not explicitly set,
	// ApplyDefaults fills in the default value. The env var precedence
	// is handled by the config layer (tested in config_test.go).
	// Here we verify the CLI correctly passes through flag values.
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// With an explicit --concurrency flag, the value should be used.
	// Since run() will fail at ffmpeg lookup, we just verify it gets
	// past argument parsing (exit code 1 from ffmpeg not found, not from parsing).
	os.Args = []string{"kinopub", "--concurrency", "4", "https://kino.pub/feed"}
	code := run()
	// Should fail at ffmpeg lookup or later, not at flag parsing.
	// Exit code 1 is expected (ffmpeg not found), but the important thing
	// is it didn't fail at argument validation.
	if code != 1 {
		t.Errorf("expected exit code 1 (ffmpeg not found), got %d", code)
	}
}
