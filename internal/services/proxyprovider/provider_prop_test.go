package proxyprovider

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// **Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5**

// --- Generators ---

// genValidHost generates a valid hostname (alphanumeric labels separated by dots).
func genValidHost(t *rapid.T) string {
	numLabels := rapid.IntRange(1, 3).Draw(t, "numLabels")
	labels := make([]string, numLabels)
	for i := range labels {
		labels[i] = rapid.StringMatching(`[a-z][a-z0-9]{0,10}`).Draw(t, fmt.Sprintf("label%d", i))
	}
	return strings.Join(labels, ".")
}

// genPort generates an optional port suffix.
func genPort(t *rapid.T) string {
	hasPort := rapid.Bool().Draw(t, "hasPort")
	if !hasPort {
		return ""
	}
	port := rapid.IntRange(1, 65535).Draw(t, "port")
	return fmt.Sprintf(":%d", port)
}

// genValidProxyURL generates a valid proxy URL with a supported scheme.
func genValidProxyURL(t *rapid.T) string {
	scheme := rapid.SampledFrom([]string{"http", "https", "socks5"}).Draw(t, "scheme")
	host := genValidHost(t)
	port := genPort(t)
	return fmt.Sprintf("%s://%s%s", scheme, host, port)
}

// genSystemEnvKey generates one of the recognized system proxy env var keys.
func genSystemEnvKey(t *rapid.T) string {
	return rapid.SampledFrom([]string{
		"HTTP_PROXY", "http_proxy",
		"HTTPS_PROXY", "https_proxy",
		"ALL_PROXY", "all_proxy",
	}).Draw(t, "envKey")
}

// genUnsupportedScheme generates a scheme that is NOT http, https, or socks5.
func genUnsupportedScheme(t *rapid.T) string {
	return rapid.SampledFrom([]string{
		"ftp", "gopher", "ws", "wss", "ssh", "telnet", "file", "smtp", "imap",
	}).Draw(t, "unsupportedScheme")
}

// --- Property 17: Proxy selection follows precedence ---
//
// For any valid explicit proxy URL and any system env proxy, the provider
// always selects explicit when provided. When explicit is empty and system env
// has a proxy, system is selected. When both are empty, direct is selected.

func TestProperty17_ExplicitAlwaysTakesPrecedence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		explicitURL := genValidProxyURL(t)

		// System env may or may not have a proxy — shouldn't matter.
		hasSystemProxy := rapid.Bool().Draw(t, "hasSystemProxy")
		env := map[string]string{}
		if hasSystemProxy {
			sysKey := genSystemEnvKey(t)
			sysURL := genValidProxyURL(t)
			env[sysKey] = sysURL
		}

		p, err := New(explicitURL, WithEnvLookup(fakeEnv(env)))
		if err != nil {
			t.Fatalf("New(%q) returned unexpected error: %v", explicitURL, err)
		}

		if p.Mode() != domain.ProxyExplicit {
			t.Fatalf("expected ProxyExplicit when explicit URL %q provided, got %v", explicitURL, p.Mode())
		}
	})
}

func TestProperty17_SystemSelectedWhenNoExplicit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sysKey := genSystemEnvKey(t)
		sysURL := genValidProxyURL(t)
		env := map[string]string{sysKey: sysURL}

		p, err := New("", WithEnvLookup(fakeEnv(env)))
		if err != nil {
			t.Fatalf("New(\"\") with system proxy returned unexpected error: %v", err)
		}

		if p.Mode() != domain.ProxySystem {
			t.Fatalf("expected ProxySystem when system env %s=%q set, got %v", sysKey, sysURL, p.Mode())
		}
	})
}

func TestProperty17_DirectWhenBothEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate an env with no proxy-related keys (only irrelevant keys).
		numKeys := rapid.IntRange(0, 5).Draw(t, "numKeys")
		env := map[string]string{}
		for i := 0; i < numKeys; i++ {
			key := rapid.StringMatching(`[A-Z_]{3,10}`).Draw(t, fmt.Sprintf("key%d", i))
			// Ensure it's not a proxy env var.
			upper := strings.ToUpper(key)
			if upper == "HTTP_PROXY" || upper == "HTTPS_PROXY" || upper == "ALL_PROXY" ||
				upper == "NO_PROXY" {
				continue
			}
			env[key] = rapid.String().Draw(t, fmt.Sprintf("val%d", i))
		}

		p, err := New("", WithEnvLookup(fakeEnv(env)))
		if err != nil {
			t.Fatalf("New(\"\") with no proxy env returned unexpected error: %v", err)
		}

		if p.Mode() != domain.ProxyDirect {
			t.Fatalf("expected ProxyDirect when no proxy configured, got %v", p.Mode())
		}
	})
}

// --- Property 18: Invalid proxy URLs are rejected ---
//
// For any URL with an unsupported scheme (not http/https/socks5) or missing
// host, New() returns ErrInvalidProxyURL.

func TestProperty18_UnsupportedSchemeRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		scheme := genUnsupportedScheme(t)
		host := genValidHost(t)
		port := genPort(t)
		proxyURL := fmt.Sprintf("%s://%s%s", scheme, host, port)

		_, err := New(proxyURL)
		if err == nil {
			t.Fatalf("New(%q) should have returned an error for unsupported scheme %q", proxyURL, scheme)
		}
		if !errors.Is(err, domain.ErrInvalidProxyURL) {
			t.Fatalf("New(%q) error = %v, want ErrInvalidProxyURL", proxyURL, err)
		}
	})
}

func TestProperty18_MissingHostRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		scheme := rapid.SampledFrom([]string{"http", "https", "socks5"}).Draw(t, "scheme")
		// URL with scheme but no host.
		proxyURL := scheme + "://"

		_, err := New(proxyURL)
		if err == nil {
			t.Fatalf("New(%q) should have returned an error for missing host", proxyURL)
		}
		if !errors.Is(err, domain.ErrInvalidProxyURL) {
			t.Fatalf("New(%q) error = %v, want ErrInvalidProxyURL", proxyURL, err)
		}
	})
}

func TestProperty18_NoSchemeRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		host := genValidHost(t)
		port := genPort(t)
		// URL without a scheme — Go's url.Parse treats this as a path, not a host.
		proxyURL := host + port

		_, err := New(proxyURL)
		if err == nil {
			t.Fatalf("New(%q) should have returned an error for missing scheme", proxyURL)
		}
		if !errors.Is(err, domain.ErrInvalidProxyURL) {
			t.Fatalf("New(%q) error = %v, want ErrInvalidProxyURL", proxyURL, err)
		}
	})
}

// --- Property 19: NO_PROXY exclusion ---
//
// For any set of NO_PROXY hosts and any target hostname, isExcluded correctly
// identifies whether the host should bypass the proxy (exact match, suffix
// match, wildcard).

func TestProperty19_ExactMatchExcludes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a list of NO_PROXY hosts.
		numHosts := rapid.IntRange(1, 5).Draw(t, "numHosts")
		hosts := make([]string, numHosts)
		for i := range hosts {
			hosts[i] = strings.ToLower(genValidHost(t))
		}

		// Pick one of the hosts — it should be excluded.
		idx := rapid.IntRange(0, len(hosts)-1).Draw(t, "idx")
		target := hosts[idx]

		p := &Provider{}
		if !p.isExcluded(target, hosts) {
			t.Fatalf("expected %q to be excluded (exact match) from %v", target, hosts)
		}
	})
}

func TestProperty19_SuffixMatchWithDotPrefix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a base domain.
		baseDomain := genValidHost(t)
		// Create a dot-prefixed pattern.
		pattern := "." + strings.ToLower(baseDomain)

		// Generate a subdomain that should match.
		sub := rapid.StringMatching(`[a-z][a-z0-9]{0,5}`).Draw(t, "sub")
		target := sub + "." + strings.ToLower(baseDomain)

		p := &Provider{}
		hosts := []string{pattern}

		if !p.isExcluded(target, hosts) {
			t.Fatalf("expected %q to be excluded by suffix pattern %q", target, pattern)
		}
	})
}

func TestProperty19_SuffixMatchWithoutDotPrefix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a base domain.
		baseDomain := strings.ToLower(genValidHost(t))
		// Pattern without leading dot — Go convention: "example.com" matches "sub.example.com".
		pattern := baseDomain

		// Generate a subdomain that should match via suffix.
		sub := rapid.StringMatching(`[a-z][a-z0-9]{0,5}`).Draw(t, "sub")
		target := sub + "." + baseDomain

		p := &Provider{}
		hosts := []string{pattern}

		if !p.isExcluded(target, hosts) {
			t.Fatalf("expected %q to be excluded by suffix pattern %q (Go convention)", target, pattern)
		}
	})
}

func TestProperty19_WildcardExcludesEverything(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		target := genValidHost(t)

		p := &Provider{}
		hosts := []string{"*"}

		if !p.isExcluded(target, hosts) {
			t.Fatalf("expected %q to be excluded by wildcard *", target)
		}
	})
}

func TestProperty19_NonMatchingHostNotExcluded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a NO_PROXY list with specific patterns.
		numHosts := rapid.IntRange(1, 3).Draw(t, "numHosts")
		hosts := make([]string, numHosts)
		for i := range hosts {
			hosts[i] = strings.ToLower(genValidHost(t)) + ".excluded"
		}

		// Generate a target that definitely doesn't match any pattern.
		// Use a completely different TLD to ensure no suffix match.
		target := strings.ToLower(genValidHost(t)) + ".notexcluded"

		p := &Provider{}
		if p.isExcluded(target, hosts) {
			t.Fatalf("expected %q to NOT be excluded from %v", target, hosts)
		}
	})
}

func TestProperty19_CaseInsensitive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		host := genValidHost(t)
		// Pattern in lowercase.
		pattern := strings.ToLower(host)
		// Target in uppercase.
		target := strings.ToUpper(host)

		p := &Provider{}
		hosts := []string{pattern}

		if !p.isExcluded(target, hosts) {
			t.Fatalf("expected case-insensitive match: %q should be excluded by %q", target, pattern)
		}
	})
}
