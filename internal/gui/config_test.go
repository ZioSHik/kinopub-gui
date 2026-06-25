package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestSplitShellArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"-c copy", []string{"-c", "copy"}},
		{"  -c   copy  ", []string{"-c", "copy"}},
		{`-metadata title="My Movie"`, []string{"-metadata", "title=My Movie"}},
		{`-x 'single quoted arg'`, []string{"-x", "single quoted arg"}},
		{`a"b"c`, []string{"abc"}},                       // quotes are removed, parts joined
		{"\t-a\tb", []string{"-a", "b"}},                 // tabs are separators
		{`-f "a b" 'c d'`, []string{"-f", "a b", "c d"}}, // mixed quoting
		{`"unterminated`, []string{"unterminated"}},      // unterminated quote → still flushed
	}
	for _, c := range cases {
		got := splitShellArgs(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitShellArgs(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestDefaultSettings(t *testing.T) {
	s := defaultSettings()
	if s.Quality != "1080p" || s.Container != "mkv" || s.Concurrency != 2 || s.Retries != 5 {
		t.Errorf("unexpected defaults: %+v", s)
	}
	if s.Verbosity != "normal" || s.Theme != "cinematic" {
		t.Errorf("unexpected defaults: %+v", s)
	}
	if s.MaxActiveJobs != 0 {
		t.Errorf("MaxActiveJobs default = %d, want 0 (unlimited)", s.MaxActiveJobs)
	}
}

func TestSettingsSaveClamps(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := newSettingsStore()

	cases := []struct {
		name string
		in   Settings
		want Settings // only the clamped fields checked below
	}{
		{"concurrency too low", Settings{Concurrency: 0, Container: "mkv"}, Settings{Concurrency: 1}},
		{"concurrency too high", Settings{Concurrency: 99, Container: "mkv"}, Settings{Concurrency: 16}},
		{"negative retries", Settings{Concurrency: 2, Retries: -3, Container: "mkv"}, Settings{Retries: 0}},
		{"negative interval", Settings{Concurrency: 2, MinIntervalMS: -5, Container: "mkv"}, Settings{MinIntervalMS: 0}},
		{"bad container → mkv", Settings{Concurrency: 2, Container: "avi"}, Settings{Container: "mkv"}},
		{"mp4 preserved", Settings{Concurrency: 2, Container: "mp4"}, Settings{Container: "mp4"}},
		{"negative maxactive → 0", Settings{Concurrency: 2, Container: "mkv", MaxActiveJobs: -1}, Settings{MaxActiveJobs: 0}},
		{"maxactive too high → 16", Settings{Concurrency: 2, Container: "mkv", MaxActiveJobs: 99}, Settings{MaxActiveJobs: 16}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := store.save(c.in)
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			if c.want.Concurrency != 0 && got.Concurrency != c.want.Concurrency {
				t.Errorf("Concurrency = %d, want %d", got.Concurrency, c.want.Concurrency)
			}
			if c.name == "negative retries" && got.Retries != 0 {
				t.Errorf("Retries = %d, want 0", got.Retries)
			}
			if c.name == "negative interval" && got.MinIntervalMS != 0 {
				t.Errorf("MinIntervalMS = %d, want 0", got.MinIntervalMS)
			}
			if c.want.Container != "" && got.Container != c.want.Container {
				t.Errorf("Container = %q, want %q", got.Container, c.want.Container)
			}
			if (c.name == "negative maxactive → 0" || c.name == "maxactive too high → 16") && got.MaxActiveJobs != c.want.MaxActiveJobs {
				t.Errorf("MaxActiveJobs = %d, want %d", got.MaxActiveJobs, c.want.MaxActiveJobs)
			}
		})
	}
}

func TestSettingsSavePersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	store := newSettingsStore()

	in := Settings{
		OutputPath:    "/tmp/out",
		Quality:       "720p",
		Container:     "mp4",
		Concurrency:   4,
		Retries:       3,
		MinIntervalMS: 250,
		Proxy:         "socks5://localhost:1080",
		Verbosity:     "verbose",
		Theme:         "dark",
		LibraryDirs:   []string{"/a", "/b"},
		MaxActiveJobs: 3,
	}
	if _, err := store.save(in); err != nil {
		t.Fatalf("save: %v", err)
	}

	// A fresh store loads the persisted file (merged over defaults).
	reloaded := newSettingsStore()
	got := reloaded.get()
	if got.OutputPath != "/tmp/out" || got.Quality != "720p" || got.Container != "mp4" {
		t.Errorf("reloaded mismatch: %+v", got)
	}
	if got.Concurrency != 4 || got.Retries != 3 || got.MinIntervalMS != 250 {
		t.Errorf("reloaded numeric mismatch: %+v", got)
	}
	if got.MaxActiveJobs != 3 || !reflect.DeepEqual(got.LibraryDirs, []string{"/a", "/b"}) {
		t.Errorf("reloaded list/maxactive mismatch: %+v", got)
	}
}

func TestSettingsLoadMergesDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Write a partial settings file with only one field set; the rest must default.
	cfgDir := filepath.Join(dir, "kinopub")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := map[string]any{"quality": "480p", "maxActiveJobs": 100} // 100 clamps to 16
	data, _ := json.Marshal(partial)
	if err := os.WriteFile(filepath.Join(cfgDir, "gui.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	store := newSettingsStore()
	got := store.get()
	if got.Quality != "480p" {
		t.Errorf("Quality = %q, want 480p", got.Quality)
	}
	// Defaulted fields.
	if got.Container != "mkv" || got.Concurrency != 2 || got.Retries != 5 {
		t.Errorf("defaults not merged: %+v", got)
	}
	if got.MaxActiveJobs != 16 {
		t.Errorf("MaxActiveJobs = %d, want clamped 16", got.MaxActiveJobs)
	}
}

func TestSettingsLoadIgnoresBadJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "kinopub")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "gui.json"), []byte("{not json"), 0o644)

	store := newSettingsStore()
	got := store.get()
	// Falls back to defaults rather than crashing.
	if got.Quality != "1080p" || got.Concurrency != 2 {
		t.Errorf("bad JSON should yield defaults, got %+v", got)
	}
}

func TestBuildRunConfig_AudioSpecsSupersede(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := buildRunConfig(RunRequest{
		URL: "https://kino.pub/item/view/1",
		AudioSpecs: []AudioSpecDTO{
			{Require: []string{"LostFilm"}, Forbid: []string{"AC3"}},
			{Require: nil}, // empty Require → skipped
		},
	})
	if err != nil {
		t.Fatalf("buildRunConfig: %v", err)
	}
	if len(cfg.AudioPref.Specs) != 1 {
		t.Fatalf("expected 1 spec (empty-require skipped), got %d", len(cfg.AudioPref.Specs))
	}
	got := cfg.AudioPref.Specs[0]
	if !reflect.DeepEqual(got.Require, []string{"LostFilm"}) || !reflect.DeepEqual(got.Forbid, []string{"AC3"}) {
		t.Errorf("spec = %+v", got)
	}
}

func TestBuildRunConfig_DefaultUserAgentAndTimeout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := buildRunConfig(RunRequest{URL: "https://kino.pub/item/view/1", UserAgent: "  "})
	if err != nil {
		t.Fatalf("buildRunConfig: %v", err)
	}
	if cfg.UserAgent != defaultUserAgent {
		t.Errorf("blank UA should default, got %q", cfg.UserAgent)
	}
	if cfg.AudioMenuTimeout == 0 {
		t.Error("AudioMenuTimeout should be defaulted to non-zero")
	}
	if !cfg.UseAPI {
		t.Error("UseAPI should be true")
	}
}

func TestBuildRunConfig_VerbosityMapping(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// "quiet" must survive end-to-end: buildRunConfig maps it to VerbosityQuiet
	// and ApplyDefaults preserves it, because VerbosityNormal (not Quiet) is the
	// zero value treated as "unset". Empty/unknown verbosity falls back to normal.
	cases := map[string]domain.Verbosity{
		"quiet":   domain.VerbosityQuiet,
		"verbose": domain.VerbosityVerbose,
		"":        domain.VerbosityNormal,
		"weird":   domain.VerbosityNormal,
	}
	for in, want := range cases {
		cfg, err := buildRunConfig(RunRequest{URL: "https://kino.pub/item/view/1", Verbosity: in})
		if err != nil {
			t.Fatalf("buildRunConfig(%q): %v", in, err)
		}
		if cfg.Verbosity != want {
			t.Errorf("verbosity %q → %v, want %v", in, cfg.Verbosity, want)
		}
	}
}

func TestBuildRunConfig_FFmpegArgsSplit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := buildRunConfig(RunRequest{
		URL:        "https://kino.pub/item/view/1",
		FFmpegArgs: `-threads 2`,
	})
	if err != nil {
		t.Fatalf("buildRunConfig: %v", err)
	}
	if !reflect.DeepEqual(cfg.FFmpegExtraArgs, []string{"-threads", "2"}) {
		t.Errorf("FFmpegExtraArgs = %v", cfg.FFmpegExtraArgs)
	}
}
