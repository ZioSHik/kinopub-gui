// Package proxyprovider implements proxy selection and configuration (Req 6).
//
// Selection precedence: explicit proxy URL > system environment > direct.
package proxyprovider

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/httpx"
)

// EnvLookupFunc abstracts os.Getenv for testability.
type EnvLookupFunc func(string) string

// Provider implements domain.ProxyProvider.
type Provider struct {
	mode      domain.ProxyMode
	proxyURL  *url.URL // nil when mode == ProxyDirect
	client    *http.Client
	envLookup EnvLookupFunc
}

// Option configures the Provider constructor.
type Option func(*Provider)

// WithEnvLookup injects a custom environment variable lookup function.
func WithEnvLookup(fn EnvLookupFunc) Option {
	return func(p *Provider) {
		p.envLookup = fn
	}
}

// New creates a ProxyProvider.
//
// explicitURL is the user-supplied proxy URL (may be empty).
// If explicitURL is empty, the provider checks system environment variables.
// If no system proxy is found, direct mode is used.
//
// Returns domain.ErrInvalidProxyURL if the explicit URL is malformed.
func New(explicitURL string, opts ...Option) (*Provider, error) {
	p := &Provider{
		envLookup: defaultEnvLookup,
	}
	for _, o := range opts {
		o(p)
	}

	if explicitURL != "" {
		parsed, err := validateProxyURL(explicitURL)
		if err != nil {
			return nil, err
		}
		p.mode = domain.ProxyExplicit
		p.proxyURL = parsed
	} else if sysURL := p.systemProxyURL(); sysURL != nil {
		p.mode = domain.ProxySystem
		p.proxyURL = sysURL
	} else {
		p.mode = domain.ProxyDirect
	}

	p.client = p.buildHTTPClient()
	return p, nil
}

// HTTPClient returns an *http.Client configured with the resolved proxy.
func (p *Provider) HTTPClient() *http.Client {
	return p.client
}

// FFmpegEnv returns environment variable entries to route ffmpeg through the
// proxy. For http/https proxies it returns ["http_proxy=<url>", "https_proxy=<url>"].
// For socks5 proxies it returns domain.ErrProxyUnsupportedFFmpeg because ffmpeg
// does not natively support SOCKS5 for HTTP requests.
// For direct mode it returns nil, nil.
func (p *Provider) FFmpegEnv() ([]string, error) {
	if p.proxyURL == nil {
		return nil, nil
	}

	scheme := strings.ToLower(p.proxyURL.Scheme)
	if scheme == "socks5" {
		return nil, fmt.Errorf("%w: socks5 proxy cannot be used with ffmpeg", domain.ErrProxyUnsupportedFFmpeg)
	}

	raw := p.proxyURL.String()
	env := []string{
		"http_proxy=" + raw,
		"https_proxy=" + raw,
	}

	// Include NO_PROXY if set in the environment.
	if noProxy := p.envLookup("NO_PROXY"); noProxy != "" {
		env = append(env, "no_proxy="+noProxy)
	} else if noProxy := p.envLookup("no_proxy"); noProxy != "" {
		env = append(env, "no_proxy="+noProxy)
	}

	return env, nil
}

// Mode reports the active proxy mode.
func (p *Provider) Mode() domain.ProxyMode {
	return p.mode
}

// ProxyURL returns the configured proxy URL, or nil for direct connections.
func (p *Provider) ProxyURL() *url.URL {
	return p.proxyURL
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// validateProxyURL checks that the URL is syntactically valid, has a supported
// scheme (http, https, socks5), and has a non-empty host.
func validateProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidProxyURL, err)
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https", "socks5":
		// supported
	default:
		return nil, fmt.Errorf("%w: unsupported scheme %q (must be http, https, or socks5)", domain.ErrInvalidProxyURL, u.Scheme)
	}

	if u.Host == "" {
		return nil, fmt.Errorf("%w: host is required", domain.ErrInvalidProxyURL)
	}

	// Ensure host actually has a hostname component (not just a port).
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: host is required", domain.ErrInvalidProxyURL)
	}

	return u, nil
}

// systemProxyURL checks environment variables for a configured system proxy.
// It checks HTTP_PROXY, HTTPS_PROXY, ALL_PROXY (case-insensitive).
func (p *Provider) systemProxyURL() *url.URL {
	for _, key := range []string{
		"HTTP_PROXY", "http_proxy",
		"HTTPS_PROXY", "https_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		if val := p.envLookup(key); val != "" {
			u, err := url.Parse(val)
			if err == nil && u.Host != "" {
				return u
			}
		}
	}
	return nil
}

// buildHTTPClient constructs an *http.Client with the appropriate transport.
func (p *Provider) buildHTTPClient() *http.Client {
	switch {
	case p.proxyURL == nil:
		// Direct connection — use platform-aware dialer (fixes DNS on Android/Termux).
		return &http.Client{
			Transport: &http.Transport{
				DialContext: httpx.NewDialer().DialContext,
			},
			Timeout: 30 * time.Second,
		}

	case strings.ToLower(p.proxyURL.Scheme) == "socks5":
		return p.buildSOCKS5Client()

	default:
		// HTTP/HTTPS proxy.
		return p.buildHTTPProxyClient()
	}
}

// buildHTTPProxyClient creates a client that routes through an HTTP/HTTPS proxy,
// respecting NO_PROXY exclusions.
func (p *Provider) buildHTTPProxyClient() *http.Client {
	proxyFunc := p.httpProxyFunc()

	transport := &http.Transport{
		Proxy: proxyFunc,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// httpProxyFunc returns a proxy function that applies the configured proxy URL
// while respecting NO_PROXY exclusions.
func (p *Provider) httpProxyFunc() func(*http.Request) (*url.URL, error) {
	noProxyHosts := p.parseNoProxy()

	return func(req *http.Request) (*url.URL, error) {
		if p.isExcluded(req.URL.Hostname(), noProxyHosts) {
			return nil, nil // direct connection for excluded hosts
		}
		return p.proxyURL, nil
	}
}

// buildSOCKS5Client creates a client that routes through a SOCKS5 proxy,
// respecting NO_PROXY exclusions.
func (p *Provider) buildSOCKS5Client() *http.Client {
	noProxyHosts := p.parseNoProxy()

	// Build the SOCKS5 dialer.
	addr := p.proxyURL.Host
	var auth *proxy.Auth
	if p.proxyURL.User != nil {
		auth = &proxy.Auth{
			User: p.proxyURL.User.Username(),
		}
		if pass, ok := p.proxyURL.User.Password(); ok {
			auth.Password = pass
		}
	}

	dialer, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
	if err != nil {
		// Fallback to direct if SOCKS5 setup fails (shouldn't happen with valid URL).
		return &http.Client{Timeout: 30 * time.Second}
	}

	transport := &http.Transport{
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(address)
			if host == "" {
				host = address
			}
			if p.isExcluded(host, noProxyHosts) {
				// Direct connection for excluded hosts.
				return net.Dial(network, address)
			}
			return dialer.Dial(network, address)
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// parseNoProxy reads the NO_PROXY/no_proxy environment variable and returns
// a list of host patterns to exclude from proxying.
func (p *Provider) parseNoProxy() []string {
	noProxy := p.envLookup("NO_PROXY")
	if noProxy == "" {
		noProxy = p.envLookup("no_proxy")
	}
	if noProxy == "" {
		return nil
	}

	var hosts []string
	for _, h := range strings.Split(noProxy, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts = append(hosts, strings.ToLower(h))
		}
	}
	return hosts
}

// isExcluded checks whether a hostname matches any NO_PROXY pattern.
// Supports exact match and suffix match (with leading dot).
func (p *Provider) isExcluded(hostname string, noProxyHosts []string) bool {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return false
	}

	for _, pattern := range noProxyHosts {
		if pattern == "*" {
			return true
		}
		// Exact match.
		if hostname == pattern {
			return true
		}
		// Suffix match: ".example.com" matches "foo.example.com".
		if strings.HasPrefix(pattern, ".") && strings.HasSuffix(hostname, pattern) {
			return true
		}
		// Also match "example.com" against "sub.example.com" (Go convention).
		if !strings.HasPrefix(pattern, ".") && strings.HasSuffix(hostname, "."+pattern) {
			return true
		}
	}
	return false
}

// defaultEnvLookup uses os.Getenv for production environment variable access.
func defaultEnvLookup(key string) string {
	return os.Getenv(key)
}
