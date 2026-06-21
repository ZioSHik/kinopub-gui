// Package main is the CLI entrypoint for the kinopub downloader.
// It parses flags, builds the RunConfig, wires up all services, and
// delegates to the app composition root (Req 1.4, 7.3, 15.1, 15.2, 15.3, 16.3).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/app/kinopub"
	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/audiomenu"
	"github.com/niazlv/kinopub-downloader/internal/lib/browsercookies"
	"github.com/niazlv/kinopub-downloader/internal/lib/credstore"
	"github.com/niazlv/kinopub-downloader/internal/lib/httpx"
	"github.com/niazlv/kinopub-downloader/internal/lib/logx"
	"github.com/niazlv/kinopub-downloader/internal/lib/termx"
	"github.com/niazlv/kinopub-downloader/internal/services/doctor"
	"github.com/niazlv/kinopub-downloader/internal/services/downloader"
	"github.com/niazlv/kinopub-downloader/internal/services/feedparser"
	"github.com/niazlv/kinopub-downloader/internal/services/hlsdownloader"
	"github.com/niazlv/kinopub-downloader/internal/services/inputresolver"
	"github.com/niazlv/kinopub-downloader/internal/services/mediaresolver"
	"github.com/niazlv/kinopub-downloader/internal/services/outputlayout"
	"github.com/niazlv/kinopub-downloader/internal/services/pagescraper"
	"github.com/niazlv/kinopub-downloader/internal/lib/termuxapi"
	"github.com/niazlv/kinopub-downloader/internal/services/progress"
	"github.com/niazlv/kinopub-downloader/internal/services/proxyprovider"
	"github.com/niazlv/kinopub-downloader/internal/services/scheduler"
	"github.com/niazlv/kinopub-downloader/internal/services/statestore"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	// Handle subcommands before flag parsing.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "login":
			return runLogin(os.Args[2:])
		case "logout":
			return runLogout()
		case "doctor":
			return runDoctor(os.Args[2:])
		case "completion":
			return runCompletion(os.Args[2:])
		}
	}

	// Define flags.
	var (
		output      string
		concurrency int
		retries     int
		proxyURL    string
		quality     string
		verbosity   string
		ffmpegPath  string
		logFile     string
		container   string
		force       bool
		seasons     string
		episodes    string
		dryRun      bool
		minInterval int
		showVersion bool
		cookie      string
		userAgent   string
		headerVals  headerList
		browserCk   browserCookiesFlag
		feedFile    string
		ffmpegArgs  string
		ffmpegX     ffmpegExtraList
		noChunked   bool
		audioSel    string
		audioMenu   bool
	)

	fs := flag.NewFlagSet("kinopub", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.StringVar(&output, "output", "", "output directory path")
	fs.StringVar(&output, "o", "", "output directory path (shorthand)")
	fs.IntVar(&concurrency, "concurrency", 0, "max concurrent downloads (default: 2)")
	fs.IntVar(&concurrency, "c", 0, "max concurrent downloads (shorthand)")
	fs.IntVar(&retries, "retries", 0, "max retry count (default: 5)")
	fs.StringVar(&proxyURL, "proxy", "", "proxy URL (http, https, or socks5)")
	fs.StringVar(&quality, "quality", "", "quality preference (e.g. 1080p)")
	fs.StringVar(&quality, "q", "", "quality preference (shorthand)")
	var verbose bool
	fs.StringVar(&verbosity, "verbosity", "normal", "log verbosity: quiet, normal, verbose")
	fs.BoolVar(&verbose, "v", false, "enable verbose output")
	fs.StringVar(&ffmpegPath, "ffmpeg", "", "ffmpeg binary path (default: ffmpeg on PATH)")
	fs.StringVar(&logFile, "log-file", "", "log file path")
	fs.StringVar(&container, "container", "mkv", "output container: mkv or mp4")
	fs.BoolVar(&force, "force", false, "force re-download of completed episodes")
	fs.StringVar(&seasons, "seasons", "", "season selection (e.g. 1,3-5)")
	fs.StringVar(&episodes, "episodes", "", "episode selection (e.g. 1,3-5)")
	fs.BoolVar(&dryRun, "dry-run", false, "list episodes without downloading")
	fs.IntVar(&minInterval, "min-interval", 0, "minimum interval between requests in ms")
	fs.StringVar(&cookie, "cookie", "", "raw Cookie header value sent with every request (and to ffmpeg)")
	fs.StringVar(&userAgent, "user-agent", "", "User-Agent sent with every request (must match the browser that issued the cookies)")
	fs.Var(&headerVals, "header", "extra HTTP header 'Name: Value' (repeatable)")
	fs.Var(&browserCk, "browser-cookies", "auto-load kino.pub cookies from a browser: safari, chrome, firefox, yandex, or auto (default auto when given without a value)")
	fs.StringVar(&feedFile, "feed-file", "", "read the RSS feed from a local file instead of fetching it over the network")
	fs.StringVar(&ffmpegArgs, "ffmpeg-args", "", "extra ffmpeg arguments as a single string (advanced, e.g. \"-c:v libx265 -crf 28\")")
	fs.Var(&ffmpegX, "x", "extra ffmpeg argument (repeatable, advanced, e.g. --x \"-c:v\" --x libx265)")
	fs.BoolVar(&noChunked, "no-chunked", false, "disable chunked HTTP download (use ffmpeg streaming for all sources)")
	fs.StringVar(&audioSel, "audio", "", "audio track selection: comma-separated patterns; prefix with '!' (or '-') to exclude (e.g. \"anilibria\", \"!jpn\", \"anilibria,!jpn\")")
	fs.BoolVar(&audioMenu, "audio-menu", false, "show an interactive audio-track picker before downloading (TTY only)")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "kinopub %s — download full-fidelity video from kino.pub\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  kinopub [flags] <url>\n")
		fmt.Fprintf(os.Stderr, "  kinopub login [flags]       — save authentication credentials\n")
		fmt.Fprintf(os.Stderr, "  kinopub logout              — remove stored credentials\n")
		fmt.Fprintf(os.Stderr, "  kinopub doctor [flags]      — verify files and repair state\n")
		fmt.Fprintf(os.Stderr, "  kinopub completion <shell>  — generate shell completion script (bash, fish)\n\n")
		fmt.Fprintf(os.Stderr, "The <url> can be:\n")
		fmt.Fprintf(os.Stderr, "  • A kino.pub page link:     https://kino.pub/item/view/38290\n")
		fmt.Fprintf(os.Stderr, "                              https://kino.pub/item/view/38290/s1e1\n")
		fmt.Fprintf(os.Stderr, "  • A podcast feed link:      https://kino.pub/podcast/get/38290/TOKEN\n")
		fmt.Fprintf(os.Stderr, "  • A local RSS/XML file:     ./feed.xml\n\n")
		fmt.Fprintf(os.Stderr, "Page links are resolved automatically when credentials are available\n")
		fmt.Fprintf(os.Stderr, "(via login, --cookie, or --browser-cookies).\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nAuthentication:\n")
		fmt.Fprintf(os.Stderr, "  kino.pub is behind Cloudflare. To download, you need valid session cookies.\n")
		fmt.Fprintf(os.Stderr, "  The easiest workflow:\n")
		fmt.Fprintf(os.Stderr, "    1. Log in to kino.pub in your browser\n")
		fmt.Fprintf(os.Stderr, "    2. Copy cookies from DevTools (Network tab → request header → Cookie)\n")
		fmt.Fprintf(os.Stderr, "    3. Run: kinopub login --cookie \"paste_here\"\n")
		fmt.Fprintf(os.Stderr, "    4. Now just: kinopub https://kino.pub/item/view/38290\n\n")
		fmt.Fprintf(os.Stderr, "  On macOS with Full Disk Access granted to your terminal:\n")
		fmt.Fprintf(os.Stderr, "    kinopub login --browser-cookies safari\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  # Download a series (credentials from `kinopub login`)\n")
		fmt.Fprintf(os.Stderr, "  kinopub -o ./downloads https://kino.pub/item/view/38290\n\n")
		fmt.Fprintf(os.Stderr, "  # Download using a direct podcast feed link (no auth needed)\n")
		fmt.Fprintf(os.Stderr, "  kinopub -o ./downloads https://kino.pub/podcast/get/12345/token\n\n")
		fmt.Fprintf(os.Stderr, "  # List what would be downloaded without writing files\n")
		fmt.Fprintf(os.Stderr, "  kinopub --dry-run https://kino.pub/item/view/38290\n\n")
		fmt.Fprintf(os.Stderr, "  # Only seasons 1 and 3-5, 1080p, through a proxy\n")
		fmt.Fprintf(os.Stderr, "  kinopub --seasons 1,3-5 -q 1080p --proxy socks5://127.0.0.1:1080 <url>\n\n")
		fmt.Fprintf(os.Stderr, "  # Keep only the AniLibria dub, never the Japanese original\n")
		fmt.Fprintf(os.Stderr, "  kinopub --audio \"anilibria,!jpn\" https://kino.pub/item/view/38290\n\n")
		fmt.Fprintf(os.Stderr, "  # Pick the audio track interactively before downloading\n")
		fmt.Fprintf(os.Stderr, "  kinopub --audio-menu https://kino.pub/item/view/38290\n\n")
		fmt.Fprintf(os.Stderr, "  # One-off with explicit cookies (without saving)\n")
		fmt.Fprintf(os.Stderr, "  kinopub --cookie \"cf_clearance=...; PHPSESSID=...\" <url>\n\n")
		fmt.Fprintf(os.Stderr, "  # Use a locally saved feed file\n")
		fmt.Fprintf(os.Stderr, "  kinopub --feed-file ./feed.xml -o ./downloads\n")
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}

	// Support the space-separated form "--browser-cookies safari": because the
	// flag has an optional value, the browser name lands in the positional args.
	// If the flag was given bare and the first positional is a known browser
	// name, consume it as the flag's value.
	posArgs := fs.Args()
	if browserCk.set && browserCk.value == browsercookies.BrowserAuto && len(posArgs) > 0 {
		if isKnownBrowser(posArgs[0]) {
			browserCk.value = strings.ToLower(posArgs[0])
			posArgs = posArgs[1:]
		}
	}

	// --version
	if showVersion {
		fmt.Printf("kinopub %s\n", version)
		return 0
	}

	// Validate the positional URL argument (Req 1.4).
	// Exactly one URL is required, unless a local --feed-file is supplied, in
	// which case the URL is optional (used only to derive the series id).
	args := posArgs
	if feedFile == "" {
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "Error: %s\n\n", domain.ErrExactlyOneURL.Error())
			fs.Usage()
			return 1
		}
	} else if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "Error: at most one URL argument is allowed with --feed-file\n\n")
		fs.Usage()
		return 1
	}
	var inputURL string
	if len(args) == 1 {
		inputURL = args[0]
	}

	// Auto-detect: if the positional argument is a path to an existing file
	// (not a URL), treat it as a local feed file. This lets the user simply
	// pass a downloaded .xml file without needing --feed-file explicitly.
	if inputURL != "" && feedFile == "" && !strings.Contains(inputURL, "://") {
		if info, err := os.Stat(inputURL); err == nil && !info.IsDir() {
			feedFile = inputURL
			inputURL = "" // no URL to resolve
		}
	}

	// Parse verbosity. The -v flag overrides --verbosity to "verbose".
	if verbose {
		verbosity = "verbose"
	}
	verb, err := parseVerbosity(verbosity)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Parse container.
	cont, err := parseContainer(container)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Parse season/episode selections.
	seasonSel, err := kinopub.ParseSelection(seasons)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	episodeSel, err := kinopub.ParseSelection(episodes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Parse audio-track preference.
	audioPref, err := kinopub.ParseAudioPreference(audioSel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Resolve the Cookie header: an explicit --cookie wins; otherwise try to
	// auto-load cookies from the named browser; finally fall back to stored
	// credentials from `kinopub login`.
	resolvedCookie := cookie
	if resolvedCookie == "" && browserCk.set {
		ck, cerr := browsercookies.Load(browserCk.value, "kino.pub")
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "Error: could not load cookies from browser %q: %v\n", browserCk.value, cerr)
			return 1
		}
		resolvedCookie = ck
	}

	// Fall back to stored credentials if nothing was provided explicitly.
	if resolvedCookie == "" {
		stored, err := credstore.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load stored credentials: %v\n", err)
		} else if !stored.IsEmpty() {
			resolvedCookie = stored.Cookie
			if userAgent == "" && stored.UserAgent != "" {
				userAgent = stored.UserAgent
			}
		}
	}

	// Default User-Agent: if no explicit --user-agent was given, use a
	// realistic Safari UA. This serves two purposes:
	//  1. Cloudflare's cf_clearance is bound to the UA that solved the
	//     challenge — without a matching UA the cookie is rejected with 403.
	//  2. Even without cookies, Go's default "Go-http-client/1.1" looks
	//     suspicious to Cloudflare and may trigger challenges.
	// The user can always override with --user-agent if their cookies were
	// issued under a different browser.
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15"
	}

	// Build RunConfig.
	// Merge ffmpeg extra args: --ffmpeg-args (split by whitespace) + --x (individual).
	var extraFFmpegArgs []string
	if ffmpegArgs != "" {
		extraFFmpegArgs = append(extraFFmpegArgs, splitShellArgs(ffmpegArgs)...)
	}
	extraFFmpegArgs = append(extraFFmpegArgs, ffmpegX...)

	cfg := domain.RunConfig{
		InputURL:        inputURL,
		OutputPath:      output,
		MaxConcurrency:  concurrency,
		MaxRetries:      retries,
		MinIntervalMS:   minInterval,
		ProxyURL:        proxyURL,
		Quality:         domain.Quality(quality),
		Verbosity:       verb,
		FFmpegPath:      ffmpegPath,
		LogFilePath:     logFile,
		Container:       cont,
		ForceRedownload: force,
		SeasonSel:       seasonSel,
		EpisodeSel:      episodeSel,
		DryRun:          dryRun,
		Cookie:          resolvedCookie,
		UserAgent:       userAgent,
		Headers:         headerVals.toMap(),
		BrowserCookies:  browserCk.value,
		FeedFile:        feedFile,
		FFmpegExtraArgs: extraFFmpegArgs,
		NoChunked:       noChunked,
		AudioPref:       audioPref,
		AudioMenu:       audioMenu,
	}

	// Apply defaults and validate.
	kinopub.ApplyDefaults(&cfg)
	if err := kinopub.ValidateConfig(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Check ffmpeg availability (Req 7.3). Skipped in dry-run mode since no
	// downloads are performed.
	if !cfg.DryRun {
		if _, err := exec.LookPath(cfg.FFmpegPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", domain.ErrFFmpegNotFound.Error())
			return 1
		}
	}

	// Set up signal-driven context for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Wire up services.
	deps, cleanup, err := buildDependencies(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer cleanup()

	// Create app and run.
	app, err := kinopub.New(deps)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	_, runErr := app.Run(ctx, cfg)
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", runErr)
		return 1
	}

	return 0
}

// buildDependencies constructs all real service implementations and returns
// the Dependencies struct, a cleanup function, and any error.
func buildDependencies(cfg domain.RunConfig) (kinopub.Dependencies, func(), error) {
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// Coordinator for TTY line-discipline.
	coord := logx.NewCoordinator(os.Stderr)

	// Build logger handlers.
	handlers := buildLogHandlers(cfg, coord)

	// Open log file if configured.
	if cfg.LogFilePath != "" {
		f, err := os.OpenFile(cfg.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return kinopub.Dependencies{}, cleanup, fmt.Errorf("cannot open log file: %w", err)
		}
		cleanups = append(cleanups, func() { f.Close() })
		handlers = append(handlers, logx.NewFileHandler(f, nil))
	}

	logger := logx.New(handlers)

	// Proxy provider.
	proxyProv, err := proxyprovider.New(cfg.ProxyURL)
	if err != nil {
		return kinopub.Dependencies{}, cleanup, err
	}

	// Build the auth-aware HTTP client: wrap the proxy client so every request
	// carries the configured Cookie / User-Agent / extra headers.
	auth := domain.RequestAuth{
		Cookie:    cfg.Cookie,
		UserAgent: cfg.UserAgent,
		Headers:   cfg.Headers,
	}
	// Always include Referer: https://kino.pub/ — the CDN (digital-cdn.net)
	// requires it and will hang/timeout without it.
	if auth.Headers == nil {
		auth.Headers = make(map[string]string)
	}
	if auth.Headers["Referer"] == "" {
		auth.Headers["Referer"] = "https://kino.pub/"
	}
	httpClient := httpx.WithAuth(proxyProv.HTTPClient(), auth)

	// Input resolver — with page scraper when auth is available.
	var resolverOpts []inputresolver.Option
	if !auth.IsZero() {
		scraper := pagescraper.New(httpClient, logger)
		resolverOpts = append(resolverOpts, inputresolver.WithPageScraper(scraper))
	}
	inputRes := inputresolver.New(logger, resolverOpts...)

	// Feed parser.
	feedPars := feedparser.New(httpClient, logger)

	// Media resolver — needs a RunOutput function for ffprobe.
	mediaRes := mediaresolver.New(
		httpClient,
		makeRunOutput(),
		logger,
		auth,
	)

	// Output layout.
	layout := outputlayout.New(cfg.Container)

	// State store.
	outputDir := cfg.OutputPath
	if outputDir == "" {
		outputDir, _ = os.Getwd()
	}
	stateStr := statestore.New(outputDir, logger)

	// Downloader.
	dl := downloader.New(
		makeRunFunc(),
		proxyProv,
		logger,
		downloader.WithFFmpegPath(cfg.FFmpegPath),
		downloader.WithAuth(auth),
		downloader.WithExtraArgs(cfg.FFmpegExtraArgs),
		downloader.WithNoChunked(cfg.NoChunked),
		downloader.WithHTTPClient(httpClient),
	)

	// Scheduler.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	sched := scheduler.New(
		scheduler.Config{
			MaxConcurrency: cfg.MaxConcurrency,
			MaxRetries:     cfg.MaxRetries,
			MinIntervalMS:  cfg.MinIntervalMS,
			GracePeriod:    cfg.GracePeriod,
		},
		&realClock{},
		logger,
		rng,
	)

	// Progress reporter — choose live or log based on TTY.
	var progReporter domain.ProgressReporter
	if termx.IsTTY(os.Stderr) {
		progReporter = progress.NewLive(os.Stderr, coord)
	} else {
		progReporter = progress.NewLog(logger)
	}
	// Wrap with Termux notifications if termux-notification is available.
	progReporter = termuxapi.Wrap(progReporter)

	deps := kinopub.Dependencies{
		Logger:           logger,
		InputResolver:    inputRes,
		FeedParser:       feedPars,
		MediaResolver:    mediaRes,
		Scheduler:        sched,
		Downloader:       dl,
		ProxyProvider:    proxyProv,
		ProgressReporter: progReporter,
		StateStore:       stateStr,
		OutputLayout:     layout,
	}

	// Optional HLS pipeline: only available when auth is present (page scraping
	// requires cookies to access the player page).
	if !auth.IsZero() {
		scraper := pagescraper.New(httpClient, logger)
		hlsDl := hlsdownloader.New(httpClient, auth, logger,
			hlsdownloader.WithConcurrency(cfg.MaxConcurrency),
			hlsdownloader.WithProxy(proxyProv.ProxyURL()))
		deps.PageScraper = scraper
		deps.HLSDownloader = hlsDl
	}

	// Interactive audio-track picker. Only meaningful when the menu is enabled
	// and stdin/stderr are a real terminal.
	if cfg.AudioMenu && termx.IsTTY(os.Stdin) && termx.IsTTY(os.Stderr) {
		deps.AudioChooser = audiomenu.New(os.Stdin, os.Stderr, true)
	}

	return deps, cleanup, nil
}

// buildLogHandlers creates the console log handler based on TTY detection and verbosity.
func buildLogHandlers(cfg domain.RunConfig, coord *logx.Coordinator) []logx.Handler {
	if termx.IsTTY(os.Stderr) {
		return []logx.Handler{logx.NewTTYHandler(os.Stderr, cfg.Verbosity, coord)}
	}
	return []logx.Handler{logx.NewPlainHandler(os.Stderr, cfg.Verbosity, coord)}
}

// parseVerbosity converts a string verbosity flag to domain.Verbosity.
func parseVerbosity(s string) (domain.Verbosity, error) {
	switch s {
	case "quiet":
		return domain.VerbosityQuiet, nil
	case "normal", "":
		return domain.VerbosityNormal, nil
	case "verbose":
		return domain.VerbosityVerbose, nil
	default:
		return 0, fmt.Errorf("%w: verbosity must be quiet, normal, or verbose, got %q", domain.ErrInvalidFlag, s)
	}
}

// parseContainer converts a string container flag to domain.Container.
func parseContainer(s string) (domain.Container, error) {
	switch s {
	case "mkv", "":
		return domain.ContainerMKV, nil
	case "mp4":
		return domain.ContainerMP4, nil
	default:
		return 0, fmt.Errorf("%w: container must be mkv or mp4, got %q", domain.ErrInvalidFlag, s)
	}
}

// headerList is a repeatable string flag that collects "Name: Value" header
// entries supplied via --header.
type headerList []string

// String implements flag.Value.
func (h *headerList) String() string {
	return strings.Join(*h, ", ")
}

// Set implements flag.Value, appending each --header occurrence.
func (h *headerList) Set(v string) error {
	if !strings.Contains(v, ":") {
		return fmt.Errorf("%w: header must be in 'Name: Value' form, got %q", domain.ErrInvalidFlag, v)
	}
	*h = append(*h, v)
	return nil
}

// toMap parses the collected header entries into a map of header name to value.
func (h headerList) toMap() map[string]string {
	if len(h) == 0 {
		return nil
	}
	m := make(map[string]string, len(h))
	for _, entry := range h {
		name, value, _ := strings.Cut(entry, ":")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name != "" {
			m[name] = value
		}
	}
	return m
}

// browserCookiesFlag is a flag with an optional value. Used bare
// (--browser-cookies) it defaults to "auto"; with a value
// (--browser-cookies=safari) it selects a specific browser. Implementing
// IsBoolFlag lets the standard flag package accept it without a following
// argument, so a positional URL after it is not mistaken for its value.
type browserCookiesFlag struct {
	set   bool
	value string
}

// String implements flag.Value.
func (b *browserCookiesFlag) String() string { return b.value }

// Set implements flag.Value. An empty value (bare flag) means "auto".
func (b *browserCookiesFlag) Set(v string) error {
	b.set = true
	if v == "" || v == "true" {
		b.value = browsercookies.BrowserAuto
	} else {
		b.value = strings.ToLower(strings.TrimSpace(v))
	}
	return nil
}

// IsBoolFlag tells the flag package the value is optional.
func (b *browserCookiesFlag) IsBoolFlag() bool { return true }

// isKnownBrowser reports whether s names a browser supported for cookie loading.
func isKnownBrowser(s string) bool {
	switch strings.ToLower(s) {
	case browsercookies.BrowserAuto,
		browsercookies.BrowserSafari,
		browsercookies.BrowserChrome,
		browsercookies.BrowserFirefox,
		browsercookies.BrowserYandex:
		return true
	default:
		return false
	}
}

// realClock implements domain.Clock using the real system clock.
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) Sleep(d time.Duration)                  { time.Sleep(d) }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// makeRunOutput creates a RunOutputFunc that executes a command and captures stdout.
// On failure, stderr is included in the error message for diagnostics.
func makeRunOutput() mediaresolver.RunOutputFunc {
	return func(ctx context.Context, name string, args, env []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			errMsg := stderr.String()
			if errMsg != "" {
				return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(errMsg))
			}
			return nil, err
		}
		return out, nil
	}
}

// makeRunFunc creates a downloader.RunFunc that executes a command, streaming
// stdout to the provided writer. ffmpeg stderr is discarded in interactive mode
// to keep the progress display clean — all useful progress info comes via
// -progress pipe:1 on stdout.
func makeRunFunc() downloader.RunFunc {
	return func(ctx context.Context, name string, args, env []string, stdout io.Writer) error {
		cmd := exec.CommandContext(ctx, name, args...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		cmd.Stdout = stdout
		// Discard ffmpeg stderr: the verbose codec/stream info clutters the
		// live progress display. Errors are detected via the exit code.
		cmd.Stderr = io.Discard
		return cmd.Run()
	}
}

// ---------------------------------------------------------------------------
// Subcommands: login / logout
// ---------------------------------------------------------------------------

// runLogin saves authentication credentials encrypted to disk.
// Usage: kinopub login --cookie "..." [--user-agent "..."]
//        kinopub login --browser-cookies [safari|chrome|firefox|auto]
func runLogin(args []string) int {
	fs := flag.NewFlagSet("kinopub login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		cookie    string
		userAgent string
		browserCk browserCookiesFlag
	)

	fs.StringVar(&cookie, "cookie", "", "raw Cookie header value to store")
	fs.StringVar(&userAgent, "user-agent", "", "User-Agent to store (should match the browser that issued the cookies)")
	fs.Var(&browserCk, "browser-cookies", "auto-load cookies from a browser: safari, chrome, firefox, yandex, or auto")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Save kino.pub authentication credentials (encrypted, machine-bound).\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  kinopub login --cookie \"cf_clearance=...; _identity=...\" --user-agent \"Mozilla/5.0 ...\"\n")
		fmt.Fprintf(os.Stderr, "  kinopub login --browser-cookies safari\n\n")
		fmt.Fprintf(os.Stderr, "Credentials are stored encrypted at ~/.config/kinopub/credentials.enc\n")
		fmt.Fprintf(os.Stderr, "and can only be decrypted on this machine.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}

	// Handle --browser-cookies consuming the next positional arg.
	posArgs := fs.Args()
	if browserCk.set && browserCk.value == browsercookies.BrowserAuto && len(posArgs) > 0 {
		if isKnownBrowser(posArgs[0]) {
			browserCk.value = strings.ToLower(posArgs[0])
		}
	}

	// Resolve cookie.
	resolvedCookie := cookie
	if resolvedCookie == "" && browserCk.set {
		ck, err := browsercookies.Load(browserCk.value, "kino.pub")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not load cookies from browser %q: %v\n", browserCk.value, err)
			return 1
		}
		resolvedCookie = ck
	}

	if resolvedCookie == "" {
		fmt.Fprintf(os.Stderr, "Error: no cookies provided. Use --cookie or --browser-cookies.\n")
		fs.Usage()
		return 1
	}

	// Default UA.
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15"
	}

	creds := credstore.Credentials{
		Cookie:    resolvedCookie,
		UserAgent: userAgent,
	}

	if err := credstore.Save(creds); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "Credentials saved (encrypted, machine-bound) to ~/.config/kinopub/credentials.enc\n")
	return 0
}

// runLogout removes stored credentials.
func runLogout() int {
	if err := credstore.Clear(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Stored credentials removed.\n")
	return 0
}

// ---------------------------------------------------------------------------
// Subcommand: doctor
// ---------------------------------------------------------------------------

// runDoctor verifies downloaded files against the state file and optionally
// repairs inconsistencies.
// Usage: kinopub doctor [--fix] [--clean-tmp] [--output <dir>]
func runDoctor(args []string) int {
	fs := flag.NewFlagSet("kinopub doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		outputDir   string
		fix         bool
		cleanTmp    bool
		verbose     bool
		skipProbe   bool
		ffprobePath string
		cookie      string
		userAgent   string
		browserCk   browserCookiesFlag
		proxyURL    string
	)

	fs.StringVar(&outputDir, "output", "", "output directory to check (default: current directory)")
	fs.StringVar(&outputDir, "o", "", "output directory to check (shorthand)")
	fs.BoolVar(&fix, "fix", false, "repair state file (remove broken entries, delete corrupt files)")
	fs.BoolVar(&cleanTmp, "clean-tmp", false, "delete orphan .tmp files from interrupted downloads")
	fs.BoolVar(&verbose, "v", false, "verbose output")
	fs.BoolVar(&skipProbe, "skip-probe", false, "skip duration verification (no network, faster)")
	fs.StringVar(&ffprobePath, "ffprobe", "", "ffprobe binary path (default: ffprobe on PATH)")
	fs.StringVar(&cookie, "cookie", "", "Cookie header for resolving source")
	fs.StringVar(&userAgent, "user-agent", "", "User-Agent for resolving source")
	fs.Var(&browserCk, "browser-cookies", "auto-load cookies: safari, chrome, firefox, yandex, or auto")
	fs.StringVar(&proxyURL, "proxy", "", "proxy URL (http, https, or socks5)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Verify downloaded files against the state file and repair inconsistencies.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  kinopub doctor [flags]\n\n")
		fmt.Fprintf(os.Stderr, "The doctor checks for:\n")
		fmt.Fprintf(os.Stderr, "  • Files recorded as completed but missing on disk\n")
		fmt.Fprintf(os.Stderr, "  • Files that are truncated (smaller than recorded size)\n")
		fmt.Fprintf(os.Stderr, "  • Files whose duration doesn't match the source\n")
		fmt.Fprintf(os.Stderr, "    (resolves fresh media URLs via the same pipeline as download)\n")
		fmt.Fprintf(os.Stderr, "  • State entries with no file path (incomplete records)\n")
		fmt.Fprintf(os.Stderr, "  • Orphan .tmp files from interrupted downloads\n\n")
		fmt.Fprintf(os.Stderr, "Duration verification resolves the series from the source (page_link/feed_url\n")
		fmt.Fprintf(os.Stderr, "in state metadata), gets fresh media URLs, probes them with ffprobe, and\n")
		fmt.Fprintf(os.Stderr, "compares with local file duration. No hardcoded thresholds.\n\n")
		fmt.Fprintf(os.Stderr, "With --fix, broken state entries are removed and corrupt files deleted,\n")
		fmt.Fprintf(os.Stderr, "so the next download run will re-download the affected episodes.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}

	// Handle --browser-cookies consuming the next positional arg.
	posArgs := fs.Args()
	if browserCk.set && browserCk.value == browsercookies.BrowserAuto && len(posArgs) > 0 {
		if isKnownBrowser(posArgs[0]) {
			browserCk.value = strings.ToLower(posArgs[0])
		}
	}

	if outputDir == "" {
		outputDir, _ = os.Getwd()
	}

	// Resolve auth (same logic as main download command).
	resolvedCookie := cookie
	if resolvedCookie == "" && browserCk.set {
		ck, cerr := browsercookies.Load(browserCk.value, "kino.pub")
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load cookies from browser %q: %v\n", browserCk.value, cerr)
		} else {
			resolvedCookie = ck
		}
	}
	if resolvedCookie == "" {
		stored, err := credstore.Load()
		if err == nil && !stored.IsEmpty() {
			resolvedCookie = stored.Cookie
			if userAgent == "" && stored.UserAgent != "" {
				userAgent = stored.UserAgent
			}
		}
	}
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15"
	}

	auth := domain.RequestAuth{
		Cookie:    resolvedCookie,
		UserAgent: userAgent,
		Headers:   map[string]string{"Referer": "https://kino.pub/"},
	}

	// Set up logger.
	coord := logx.NewCoordinator(os.Stderr)
	var handlers []logx.Handler
	verb := domain.VerbosityNormal
	if verbose {
		verb = domain.VerbosityVerbose
	}
	if termx.IsTTY(os.Stderr) {
		handlers = append(handlers, logx.NewTTYHandler(os.Stderr, verb, coord))
	} else {
		handlers = append(handlers, logx.NewPlainHandler(os.Stderr, verb, coord))
	}
	logger := logx.New(handlers)

	// Wire up dependencies — same services as the main download command.
	proxyProv, err := proxyprovider.New(proxyURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	httpClient := httpx.WithAuth(proxyProv.HTTPClient(), auth)

	var resolverOpts []inputresolver.Option
	if !auth.IsZero() {
		scraper := pagescraper.New(httpClient, logger)
		resolverOpts = append(resolverOpts, inputresolver.WithPageScraper(scraper))
	}
	inputRes := inputresolver.New(logger, resolverOpts...)
	feedPars := feedparser.New(httpClient, logger)
	mediaRes := mediaresolver.New(
		httpClient,
		makeRunOutput(),
		logger,
		auth,
	)

	deps := doctor.Deps{
		Logger:        logger,
		InputResolver: inputRes,
		FeedParser:    feedPars,
		MediaResolver: mediaRes,
	}

	opts := doctor.Options{
		OutputDir:   outputDir,
		Fix:         fix,
		CleanTmp:    cleanTmp,
		SkipProbe:   skipProbe,
		FFprobePath: ffprobePath,
	}

	report, err := doctor.Run(context.Background(), deps, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Print report.
	printDoctorReport(report, fix)

	if report.HasIssues() && !fix {
		return 1
	}
	return 0
}

// printDoctorReport outputs the doctor findings to stderr.
func printDoctorReport(report *doctor.Report, fixed bool) {
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Doctor Report\n")
	fmt.Fprintf(os.Stderr, "─────────────\n")

	if report.SeriesTitle != "" {
		fmt.Fprintf(os.Stderr, "Series:     %s\n", report.SeriesTitle)
	}
	if report.SeriesID != "" {
		fmt.Fprintf(os.Stderr, "Series ID:  %s\n", report.SeriesID)
	}
	fmt.Fprintf(os.Stderr, "State file: %s\n", report.StateFile)
	fmt.Fprintf(os.Stderr, "Entries:    %d total, %d healthy\n", report.TotalInState, report.Healthy)
	if report.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "Skipped:    %d (remote links expired, could not verify duration)\n", report.Skipped)
	}
	fmt.Fprintf(os.Stderr, "\n")

	if !report.HasIssues() {
		fmt.Fprintf(os.Stderr, "✓ All files are consistent with the state file.\n\n")
		return
	}

	// Group issues by kind for cleaner output.
	byKind := make(map[doctor.IssueKind][]doctor.Issue)
	for _, issue := range report.Issues {
		byKind[issue.Kind] = append(byKind[issue.Kind], issue)
	}

	kindOrder := []doctor.IssueKind{
		doctor.IssueMissing,
		doctor.IssueTruncated,
		doctor.IssueDurationMismatch,
		doctor.IssueSizeMismatch,
		doctor.IssueNoPath,
		doctor.IssueOrphanTmp,
	}

	for _, kind := range kindOrder {
		issues := byKind[kind]
		if len(issues) == 0 {
			continue
		}

		fmt.Fprintf(os.Stderr, "  %s (%d):\n", kind.String(), len(issues))
		for _, issue := range issues {
			if issue.Key != "" {
				fmt.Fprintf(os.Stderr, "    • %s: %s\n", issue.Key, issue.Detail)
			} else {
				fmt.Fprintf(os.Stderr, "    • %s\n", issue.Detail)
			}
		}
		fmt.Fprintf(os.Stderr, "\n")
	}

	if fixed {
		fmt.Fprintf(os.Stderr, "✓ State file repaired. Run the download command again to re-download affected episodes.\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "Run with --fix to repair the state file (broken entries will be removed\n")
		fmt.Fprintf(os.Stderr, "so the next download re-fetches affected episodes).\n\n")
	}
}

// ffmpegExtraList is a repeatable string flag that collects individual ffmpeg
// arguments supplied via --x.
type ffmpegExtraList []string

// String implements flag.Value.
func (f *ffmpegExtraList) String() string {
	return strings.Join(*f, " ")
}

// Set implements flag.Value, appending each --x occurrence.
func (f *ffmpegExtraList) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// ---------------------------------------------------------------------------
// Subcommand: completion
// ---------------------------------------------------------------------------

// runCompletion prints a shell completion script to stdout.
// Usage: kinopub completion bash
//
//	kinopub completion fish
func runCompletion(args []string) int {
	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	switch shell {
	case "fish":
		fmt.Print(fishCompletion)
	case "bash":
		fmt.Print(bashCompletion)
	default:
		fmt.Fprintf(os.Stderr, "Usage: kinopub completion <shell>\n\n")
		fmt.Fprintf(os.Stderr, "Available shells:\n")
		fmt.Fprintf(os.Stderr, "  bash   — source <(kinopub completion bash)\n")
		fmt.Fprintf(os.Stderr, "  fish   — kinopub completion fish | source\n\n")
		fmt.Fprintf(os.Stderr, "To install permanently:\n")
		fmt.Fprintf(os.Stderr, "  bash:  kinopub completion bash >> ~/.bashrc\n")
		fmt.Fprintf(os.Stderr, "  fish:  kinopub completion fish > ~/.config/fish/completions/kinopub.fish\n")
		if shell != "" {
			return 1
		}
	}
	return 0
}

const fishCompletion = `# kinopub fish shell completion
# Install: kinopub completion fish > ~/.config/fish/completions/kinopub.fish

set -l subcommands login logout doctor completion

# Subcommands
complete -c kinopub -f -n "not __fish_seen_subcommand_from $subcommands" -a login      -d "Save authentication credentials"
complete -c kinopub -f -n "not __fish_seen_subcommand_from $subcommands" -a logout     -d "Remove stored credentials"
complete -c kinopub -f -n "not __fish_seen_subcommand_from $subcommands" -a doctor     -d "Verify files and repair state"
complete -c kinopub -f -n "not __fish_seen_subcommand_from $subcommands" -a completion -d "Generate shell completion script"

# Main command flags
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands" -s o -l output        -d "Output directory path" -r -F
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands" -s c -l concurrency   -d "Max concurrent downloads" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l retries        -d "Max retry count" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l proxy          -d "Proxy URL (http, https, socks5)" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands" -s q -l quality       -d "Quality preference" -r -a "4k 2160p 1080p 720p 480p 360p"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l verbosity      -d "Log verbosity" -r -a "quiet\t'Suppress output' normal\t'Default' verbose\t'All messages'"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands" -s v                  -d "Verbose output"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l ffmpeg         -d "ffmpeg binary path" -r -F
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l log-file       -d "Log file path" -r -F
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l container      -d "Output container" -r -a "mkv\t'Matroska (default)' mp4\t'MPEG-4'"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l force          -d "Force re-download of completed episodes"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l seasons        -d "Season selection (e.g. 1,3-5)" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l episodes       -d "Episode selection (e.g. 1,3-5)" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l dry-run        -d "List episodes without downloading"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l min-interval   -d "Min interval between requests (ms)" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l cookie         -d "Raw Cookie header value" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l user-agent     -d "User-Agent header" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l header         -d "Extra HTTP header 'Name: Value'" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l browser-cookies -d "Auto-load cookies from browser" -r -a "safari chrome firefox yandex auto"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l feed-file      -d "Read RSS feed from local file" -r -F
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l ffmpeg-args    -d "Extra ffmpeg arguments" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands" -s x                  -d "Extra ffmpeg argument (repeatable)" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l no-chunked     -d "Disable chunked HTTP download"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l audio          -d "Audio track selection (e.g. anilibria,!jpn)" -r
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l audio-menu     -d "Show interactive audio-track picker"
complete -c kinopub -n "not __fish_seen_subcommand_from $subcommands"      -l version        -d "Print version and exit"

# login flags
complete -c kinopub -n "__fish_seen_subcommand_from login" -l cookie          -d "Cookie header to store" -r
complete -c kinopub -n "__fish_seen_subcommand_from login" -l user-agent      -d "User-Agent to store" -r
complete -c kinopub -n "__fish_seen_subcommand_from login" -l browser-cookies -d "Auto-load cookies from browser" -r -a "safari chrome firefox yandex auto"

# doctor flags
complete -c kinopub -n "__fish_seen_subcommand_from doctor" -s o -l output         -d "Output directory to check" -r -F
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l fix             -d "Repair state file"
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l clean-tmp       -d "Delete orphan .tmp files"
complete -c kinopub -n "__fish_seen_subcommand_from doctor" -s v                   -d "Verbose output"
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l skip-probe      -d "Skip duration verification"
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l ffprobe         -d "ffprobe binary path" -r -F
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l cookie          -d "Cookie header for resolving source" -r
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l user-agent      -d "User-Agent for resolving source" -r
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l browser-cookies -d "Auto-load cookies from browser" -r -a "safari chrome firefox yandex auto"
complete -c kinopub -n "__fish_seen_subcommand_from doctor"      -l proxy           -d "Proxy URL" -r

# completion flags
complete -c kinopub -f -n "__fish_seen_subcommand_from completion" -a "bash fish"
`

const bashCompletion = `# kinopub bash shell completion
# Install: source <(kinopub completion bash)
#          or: kinopub completion bash >> ~/.bashrc

_kinopub_completion() {
    local cur prev words cword
    _init_completion || return

    local subcommands="login logout doctor completion"
    local main_flags="-o --output -c --concurrency --retries --proxy -q --quality
        --verbosity -v --ffmpeg --log-file --container --force --seasons --episodes
        --dry-run --min-interval --cookie --user-agent --header --browser-cookies
        --feed-file --ffmpeg-args -x --no-chunked --audio --audio-menu --version"

    # Detect which subcommand is active
    local subcmd=""
    for w in "${words[@]:1}"; do
        case "$w" in
            login|logout|doctor|completion)
                subcmd="$w"
                break
                ;;
        esac
    done

    case "$subcmd" in
        login)
            case "$prev" in
                --cookie|--user-agent) return ;;
                --browser-cookies) COMPREPLY=($(compgen -W "safari chrome firefox yandex auto" -- "$cur")); return ;;
            esac
            COMPREPLY=($(compgen -W "--cookie --user-agent --browser-cookies" -- "$cur"))
            ;;
        logout)
            ;;
        doctor)
            case "$prev" in
                -o|--output|--ffprobe) COMPREPLY=($(compgen -d -- "$cur")); return ;;
                --cookie|--user-agent|--proxy) return ;;
                --browser-cookies) COMPREPLY=($(compgen -W "safari chrome firefox yandex auto" -- "$cur")); return ;;
            esac
            COMPREPLY=($(compgen -W "-o --output --fix --clean-tmp -v --skip-probe
                --ffprobe --cookie --user-agent --browser-cookies --proxy" -- "$cur"))
            ;;
        completion)
            COMPREPLY=($(compgen -W "bash fish" -- "$cur"))
            ;;
        *)
            # Main command
            if [[ "$cur" == -* ]]; then
                case "$prev" in
                    -o|--output|--log-file|--feed-file|--ffmpeg)
                        COMPREPLY=($(compgen -f -- "$cur")); return ;;
                    -q|--quality)
                        COMPREPLY=($(compgen -W "4k 2160p 1080p 720p 480p 360p" -- "$cur")); return ;;
                    --container)
                        COMPREPLY=($(compgen -W "mkv mp4" -- "$cur")); return ;;
                    --verbosity)
                        COMPREPLY=($(compgen -W "quiet normal verbose" -- "$cur")); return ;;
                    --browser-cookies)
                        COMPREPLY=($(compgen -W "safari chrome firefox yandex auto" -- "$cur")); return ;;
                    --cookie|--user-agent|--proxy|--header|--seasons|--episodes| \
                    --min-interval|--retries|--ffmpeg-args|-x|-c|--concurrency|--audio)
                        return ;;
                esac
                COMPREPLY=($(compgen -W "$main_flags" -- "$cur"))
            else
                # No subcommand yet: offer subcommands + file completion for URL/path arg
                if [[ -z "$subcmd" ]]; then
                    COMPREPLY=($(compgen -W "$subcommands" -- "$cur"))
                    # Also allow files (for local feed files)
                    COMPREPLY+=($(compgen -f -- "$cur"))
                fi
            fi
            ;;
    esac
}

complete -F _kinopub_completion kinopub
`

// splitShellArgs splits a string into arguments respecting simple quoting.
// It handles double-quoted and single-quoted strings, but does not support
// escape sequences within quotes (good enough for ffmpeg args).
func splitShellArgs(s string) []string {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for _, r := range s {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t') && !inSingle && !inDouble:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
