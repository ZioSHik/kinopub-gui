package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/app/kinopub"
	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// defaultUserAgent matches the CLI: Cloudflare's cf_clearance is bound to the
// UA that solved the challenge, so we default to a realistic Safari UA.
const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15"

// Settings holds user-configurable GUI defaults persisted between sessions.
type Settings struct {
	OutputPath    string   `json:"outputPath"`
	Quality       string   `json:"quality"`
	Container     string   `json:"container"`
	Concurrency   int      `json:"concurrency"`
	Retries       int      `json:"retries"`
	MinIntervalMS int      `json:"minIntervalMs"`
	Proxy         string   `json:"proxy"`
	Verbosity     string   `json:"verbosity"`
	NoChunked     bool     `json:"noChunked"`
	Theme         string   `json:"theme"`
	LibraryDirs   []string `json:"libraryDirs"`
}

func defaultSettings() Settings {
	home, _ := os.UserHomeDir()
	out := ""
	if home != "" {
		out = filepath.Join(home, "Downloads", "kinopub")
	}
	return Settings{
		OutputPath:  out,
		Quality:     "1080p",
		Container:   "mkv",
		Concurrency: 2,
		Retries:     5,
		Verbosity:   "normal",
		Theme:       "cinematic",
		LibraryDirs: nil,
	}
}

// settingsStore persists Settings as JSON next to the encrypted credentials.
type settingsStore struct {
	mu   sync.RWMutex
	cur  Settings
	path string
}

func newSettingsStore() *settingsStore {
	s := &settingsStore{cur: defaultSettings()}
	if dir, err := configDir(); err == nil {
		s.path = filepath.Join(dir, "gui.json")
		s.load()
	}
	return s
}

func configDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "kinopub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "kinopub"), nil
}

func (s *settingsStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	// Merge over defaults so new fields keep sensible values.
	merged := defaultSettings()
	if loaded.OutputPath != "" {
		merged.OutputPath = loaded.OutputPath
	}
	if loaded.Quality != "" {
		merged.Quality = loaded.Quality
	}
	if loaded.Container != "" {
		merged.Container = loaded.Container
	}
	if loaded.Concurrency > 0 {
		merged.Concurrency = loaded.Concurrency
	}
	if loaded.Retries > 0 {
		merged.Retries = loaded.Retries
	}
	merged.MinIntervalMS = loaded.MinIntervalMS
	merged.Proxy = loaded.Proxy
	if loaded.Verbosity != "" {
		merged.Verbosity = loaded.Verbosity
	}
	merged.NoChunked = loaded.NoChunked
	if loaded.Theme != "" {
		merged.Theme = loaded.Theme
	}
	merged.LibraryDirs = loaded.LibraryDirs
	s.cur = merged
}

func (s *settingsStore) get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func (s *settingsStore) save(in Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Validate / clamp.
	if in.Concurrency < 1 {
		in.Concurrency = 1
	}
	if in.Concurrency > 16 {
		in.Concurrency = 16
	}
	if in.Retries < 0 {
		in.Retries = 0
	}
	if in.MinIntervalMS < 0 {
		in.MinIntervalMS = 0
	}
	if in.Container != "mp4" {
		in.Container = "mkv"
	}
	s.cur = in
	if s.path == "" {
		return s.cur, nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return s.cur, err
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return s.cur, err
	}
	return s.cur, os.WriteFile(s.path, data, 0o644)
}

// RunRequest is the JSON body the UI sends to start a download or run a preview.
type RunRequest struct {
	URL           string `json:"url"`
	OutputPath    string `json:"outputPath"`
	Quality       string `json:"quality"`
	Container     string `json:"container"`
	Concurrency   int    `json:"concurrency"`
	Retries       int    `json:"retries"`
	MinIntervalMS int    `json:"minIntervalMs"`
	Proxy         string `json:"proxy"`
	Seasons       string `json:"seasons"`
	Episodes      string `json:"episodes"`
	// EpisodeKeys is an explicit per-episode selection from the series browser,
	// each formatted "S{season}E{episode}". When present it overrides Seasons /
	// Episodes so the exact picked set downloads.
	EpisodeKeys []string `json:"episodeKeys"`
	Audio       string   `json:"audio"`
	AudioMenu   bool     `json:"audioMenu"`
	Force       bool     `json:"force"`
	NoChunked   bool     `json:"noChunked"`
	DryRun      bool     `json:"dryRun"`
	FFmpegArgs  string   `json:"ffmpegArgs"`
	FFmpegPath  string   `json:"ffmpegPath"`
	UserAgent   string   `json:"userAgent"`
	Verbosity   string   `json:"verbosity"`
}

// buildRunConfig translates a RunRequest into a validated domain.RunConfig.
func buildRunConfig(req RunRequest) (domain.RunConfig, error) {
	cont := domain.ContainerMKV
	if req.Container == "mp4" {
		cont = domain.ContainerMP4
	}

	verb := domain.VerbosityNormal
	switch req.Verbosity {
	case "quiet":
		verb = domain.VerbosityQuiet
	case "verbose":
		verb = domain.VerbosityVerbose
	}

	seasonSel, err := kinopub.ParseSelection(req.Seasons)
	if err != nil {
		return domain.RunConfig{}, err
	}
	episodeSel, err := kinopub.ParseSelection(req.Episodes)
	if err != nil {
		return domain.RunConfig{}, err
	}
	selectedEpisodes, err := parseEpisodeKeys(req.EpisodeKeys)
	if err != nil {
		return domain.RunConfig{}, err
	}
	audioPref, err := kinopub.ParseAudioPreference(req.Audio)
	if err != nil {
		return domain.RunConfig{}, err
	}

	ua := strings.TrimSpace(req.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}

	var extraFFmpeg []string
	if req.FFmpegArgs != "" {
		extraFFmpeg = splitShellArgs(req.FFmpegArgs)
	}

	cfg := domain.RunConfig{
		InputURL:         req.URL,
		OutputPath:       req.OutputPath,
		MaxConcurrency:   req.Concurrency,
		MaxRetries:       req.Retries,
		MinIntervalMS:    req.MinIntervalMS,
		ProxyURL:         req.Proxy,
		Quality:          domain.Quality(req.Quality),
		Verbosity:        verb,
		FFmpegPath:       req.FFmpegPath,
		Container:        cont,
		ForceRedownload:  req.Force,
		SeasonSel:        seasonSel,
		EpisodeSel:       episodeSel,
		SelectedEpisodes: selectedEpisodes,
		DryRun:           req.DryRun,
		UserAgent:        ua,
		FFmpegExtraArgs:  extraFFmpeg,
		NoChunked:        req.NoChunked,
		AudioPref:        audioPref,
		AudioMenu:        req.AudioMenu,
		UseAPI:           true,
	}

	kinopub.ApplyDefaults(&cfg)
	if err := kinopub.ValidateConfig(&cfg); err != nil {
		return domain.RunConfig{}, err
	}
	if cfg.AudioMenuTimeout == 0 {
		cfg.AudioMenuTimeout = 90 * time.Second
	}
	return cfg, nil
}

// parseEpisodeKeys parses "S{season}E{episode}" keys (as produced by the series
// browser) into domain.EpisodeKey values. Unparseable keys are an error.
func parseEpisodeKeys(keys []string) ([]domain.EpisodeKey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]domain.EpisodeKey, 0, len(keys))
	for _, k := range keys {
		var season, episode int
		if _, err := fmt.Sscanf(strings.TrimSpace(k), "S%dE%d", &season, &episode); err != nil {
			return nil, fmt.Errorf("invalid episode key %q", k)
		}
		out = append(out, domain.EpisodeKey{Season: season, Episode: episode})
	}
	return out, nil
}

// splitShellArgs splits a string into args respecting simple single/double
// quoting (mirrors the CLI helper for --ffmpeg-args).
func splitShellArgs(s string) []string {
	var args []string
	var cur []rune
	inSingle, inDouble := false, false
	flush := func() {
		if len(cur) > 0 {
			args = append(args, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t') && !inSingle && !inDouble:
			flush()
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return args
}
