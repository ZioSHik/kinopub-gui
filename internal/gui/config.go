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
	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
	"github.com/ZioSHik/kinopub-gui/internal/lib/httpx"
)

// defaultUserAgent matches the CLI: Cloudflare's cf_clearance is bound to the
// UA that solved the challenge, so we default to a realistic Safari UA.
const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15"

// Download tuning. These were user-facing knobs ("Concurrency", "Retries",
// "Max simultaneous downloads") until it became clear there is one right answer
// and the wrong ones only hurt, so they are fixed here.
//
// episodeConcurrency: this is NOT a bandwidth knob — the segment budget that
// fills the link is set by a throughput controller shared across every download
// in the process (hlsdownloader.Limiter), which measures the link and ramps itself,
// so raising this number does not buy more speed. The second episode exists
// only to keep the network busy while the first one sits in its ffmpeg remux
// phase — CPU-bound, zero network. A third would just push back the first
// finished file for no throughput in return.
//
// episodeRetries: these fire only on retryable failures (403/429/5xx, timeouts,
// connection resets, DNS) and back off 1→2→4→8→16s, so 5 means up to 6 attempts
// spread over ~31s — enough to ride out a CDN blip or a re-signed token, still
// bounded. Per-segment and per-manifest retries sit below this level and already
// absorb the short-lived failures.
//
// maxActiveDownloads: the resting limit. Bandwidth is one fixed pie, so running
// several titles at once normally only divides it — nothing finishes sooner in
// total while every single title finishes later. One at a time gives the fastest
// first watchable file; the wait queue is reorderable, and per-episode retries
// are flagged urgent so they bypass this limit and still start immediately.
//
// maxAdaptiveDownloads: the exception the admission controller may grant. There
// is one case where the resting limit leaves throughput on the table — when the
// running title is not claiming the segment budget at all, because it is
// remuxing with ffmpeg, scraping, or resolving a manifest. JobManager.admissionLoop
// watches the shared controller for exactly that and lends out the second slot
// while it lasts. It is safe to lend precisely because the segment budget is
// shared: an extra title divides that one budget instead of multiplying it.
//
// admissionPoll / admissionWindowSamples / admissionIdleShare: how that is
// measured — a sample every 2s over a ~30s window, and the pipe has to have been
// idle for most of it. The window is what keeps the seconds every job spends
// resolving from quietly making two-at-a-time the norm.
const (
	episodeConcurrency   = 2
	episodeRetries       = 5
	maxActiveDownloads   = 1
	maxAdaptiveDownloads = 2

	admissionPoll          = 2 * time.Second
	admissionWindowSamples = 15
	admissionIdleShare     = 0.6
)

// Settings holds user-configurable GUI defaults persisted between sessions.
type Settings struct {
	OutputPath  string   `json:"outputPath"`
	Quality     string   `json:"quality"`
	Container   string   `json:"container"`
	Proxy       string   `json:"proxy"`
	Verbosity   string   `json:"verbosity"`
	Theme       string   `json:"theme"`
	LibraryDirs []string `json:"libraryDirs"`
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
	merged.Proxy = loaded.Proxy
	if loaded.Verbosity != "" {
		merged.Verbosity = loaded.Verbosity
	}
	if loaded.Theme != "" {
		merged.Theme = loaded.Theme
	}
	merged.LibraryDirs = loaded.LibraryDirs
	s.cur = merged
}

func (s *settingsStore) get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSettings(s.cur)
}

func (s *settingsStore) save(in Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Validate / clamp.
	if in.Container != "mp4" {
		in.Container = "mkv"
	}
	in = cloneSettings(in)
	if s.path == "" {
		s.cur = in
		return cloneSettings(s.cur), nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return cloneSettings(s.cur), err
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return cloneSettings(s.cur), err
	}
	if err := fsutil.AtomicWrite(s.path, data, 0600); err != nil {
		return cloneSettings(s.cur), err
	}
	s.cur = in
	return cloneSettings(s.cur), nil
}

// AudioSpecDTO is one exact audio-track selection rule sent by the GUI picker:
// keep a track that contains every Require token and none of the Forbid tokens.
type AudioSpecDTO struct {
	Require []string `json:"require"`
	Forbid  []string `json:"forbid"`
}

// RunRequest is the JSON body the UI sends to start a download or run a preview.
type RunRequest struct {
	URL        string `json:"url"`
	OutputPath string `json:"outputPath"`
	Quality    string `json:"quality"`
	Container  string `json:"container"`
	Proxy      string `json:"proxy"`
	Seasons    string `json:"seasons"`
	Episodes   string `json:"episodes"`
	// EpisodeKeys is an explicit per-episode selection from the series browser,
	// each formatted "S{season}E{episode}". When present it overrides Seasons /
	// Episodes so the exact picked set downloads.
	EpisodeKeys []string `json:"episodeKeys"`
	Audio       string   `json:"audio"`
	// AudioSpecs is an exact audio-track selection from the GUI picker. When
	// present it supersedes Audio: each spec keeps tracks containing all Require
	// tokens and none of the Forbid tokens, which precisely separates codec
	// variants of one voiceover (plain stereo vs. its AC3 5.1 sibling).
	AudioSpecs []AudioSpecDTO `json:"audioSpecs"`
	AudioMenu  bool           `json:"audioMenu"`
	Force      bool           `json:"force"`
	DryRun     bool           `json:"dryRun"`
	FFmpegArgs string         `json:"ffmpegArgs"`
	FFmpegPath string         `json:"ffmpegPath"`
	UserAgent  string         `json:"userAgent"`
	Verbosity  string         `json:"verbosity"`
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
	// An exact picker selection supersedes the substring filter.
	for _, s := range req.AudioSpecs {
		if len(s.Require) == 0 {
			continue
		}
		audioPref.Specs = append(audioPref.Specs, domain.AudioSpec{Require: s.Require, Forbid: s.Forbid})
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
		// A pasted link may still use the old domain; both resolve, but the
		// queue should show one consistent (current) form.
		InputURL:       httpx.CanonicalSiteURL(req.URL),
		OutputPath:     req.OutputPath,
		MaxConcurrency: episodeConcurrency,
		MaxRetries:     episodeRetries,
		// MinIntervalMS stays 0: a fixed delay before every request only slows
		// each episode down, and a real 429 is answered by retry-with-backoff,
		// which adapts to the server instead of guessing ahead of it.
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

func cloneSettings(s Settings) Settings {
	s.LibraryDirs = append([]string(nil), s.LibraryDirs...)
	return s
}
