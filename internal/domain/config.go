package domain

import "time"

// Quality represents a video quality preference (e.g., "1080p").
// An empty string means auto/highest.
type Quality string

// Verbosity controls the minimum log level displayed on interactive output.
type Verbosity int

const (
	VerbosityQuiet   Verbosity = iota // show only warn/error
	VerbosityNormal                   // show info/warn/error (default)
	VerbosityVerbose                  // show debug/info/warn/error
)

// ProxyMode indicates how the proxy was resolved.
type ProxyMode int

const (
	ProxyDirect   ProxyMode = iota // no proxy
	ProxySystem                    // from environment variables
	ProxyExplicit                  // explicitly configured
)

// Container selects the output mux container format.
type Container int

const (
	ContainerMKV Container = iota // default — best multi-audio/subtitle support
	ContainerMP4
)

// RunConfig holds all configuration for a single download run.
type RunConfig struct {
	InputURL        string
	OutputPath      string        // "" → cwd (Req 11.1)
	MaxConcurrency  int           // [1,16], default 2 (Req 4.1, 4.2)
	MaxRetries      int           // default 5 (Req 5.6)
	MinIntervalMS   int           // [0,60000] (Req 4.5)
	ProxyURL        string        // explicit proxy; "" → system/direct
	Quality         Quality
	Verbosity       Verbosity     // default Normal (Req 14.1)
	FFmpegPath      string        // default "ffmpeg" on PATH (Req 7.3)
	LogFilePath     string        // "" → no file sink (Req 13.7)
	Container       Container
	ForceRedownload bool          // (Req 12.4)
	SeasonSel       Selection     // (Req 15.5)
	EpisodeSel      Selection     // (Req 15.5)
	DryRun          bool          // (Req 15.6)
	GracePeriod     time.Duration // default 30s (Req 4.7)

	// Authentication / request shaping. kino.pub sits behind Cloudflare and may
	// return HTTP 403 for unauthenticated requests. These fields let the user
	// supply credentials captured from a logged-in browser session so the tool
	// and ffmpeg can issue requests that pass Cloudflare and kino.pub auth.
	Cookie         string            // raw Cookie header value applied to all requests
	UserAgent      string            // User-Agent applied to all requests (must match the cf_clearance UA)
	Headers        map[string]string // extra HTTP headers applied to all requests
	BrowserCookies string            // browser name to auto-load kino.pub cookies from ("", "safari", "chrome", "firefox", "auto")

	// FeedFile, when set, is a path to a locally saved RSS feed file. It is used
	// instead of fetching the feed over the network — useful when the feed URL
	// returns 403. The InputURL is still used to derive the SeriesID when present.
	FeedFile string

	// FFmpegExtraArgs are additional arguments passed to ffmpeg before the output
	// path. This allows advanced users to override encoding settings (e.g.
	// transcode on the fly) or add filters.
	FFmpegExtraArgs []string

	// NoChunked disables the chunked HTTP download mode. When false (default),
	// progressive MP4 sources are downloaded via HTTP Range requests with
	// resume capability. When true, all downloads go through ffmpeg directly.
	NoChunked bool

	// AudioPref selects which audio tracks to keep. The zero value keeps every
	// track. See AudioPreference for matching semantics. (audio selection)
	AudioPref AudioPreference

	// AudioMenu enables the interactive audio-track picker shown before the
	// first download. When the user makes no choice within AudioMenuTimeout,
	// all tracks are kept. The menu is only shown on a TTY.
	AudioMenu bool
	// AudioMenuTimeout bounds how long the interactive picker waits for input
	// before defaulting to "keep all". Zero means use the package default.
	AudioMenuTimeout time.Duration
}

// RequestAuth carries credentials and request-shaping headers applied to every
// outbound HTTP request (and propagated to ffmpeg). It exists so the tool can
// reuse a logged-in browser session to pass Cloudflare and kino.pub auth.
type RequestAuth struct {
	Cookie    string            // raw Cookie header value
	UserAgent string            // User-Agent (must match the cf_clearance UA)
	Headers   map[string]string // extra headers
}

// IsZero reports whether the auth carries no information.
func (a RequestAuth) IsZero() bool {
	return a.Cookie == "" && a.UserAgent == "" && len(a.Headers) == 0
}

// Selection is a parsed set/range expression over season or episode numbers.
type Selection struct {
	All    bool
	Values map[int]bool
	Ranges []SelectionRange
}

// SelectionRange represents a contiguous inclusive range [Lo, Hi].
type SelectionRange struct {
	Lo, Hi int
}

// Matches returns true if n is included in the selection.
// An empty selection (All=false, no Values, no Ranges) matches nothing.
// When All is true, every n matches.
func (s Selection) Matches(n int) bool {
	if s.All {
		return true
	}
	if s.Values[n] {
		return true
	}
	for _, r := range s.Ranges {
		if n >= r.Lo && n <= r.Hi {
			return true
		}
	}
	return false
}
