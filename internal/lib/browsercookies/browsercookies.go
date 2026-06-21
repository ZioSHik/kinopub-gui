// Package browsercookies reads cookies for a given domain from local browser
// cookie stores (Safari, Chrome, Firefox) and assembles a Cookie header value.
//
// This is a best-effort convenience: browser cookie stores are
// vaguely-documented, OS-specific, and may be encrypted (Chrome on macOS
// requires Keychain access). Failures are returned as errors so callers can
// fall back to an explicitly supplied --cookie value.
package browsercookies

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register cookie store finders
)

// Supported browser identifiers accepted by Load.
const (
	BrowserAuto    = "auto"
	BrowserSafari  = "safari"
	BrowserChrome  = "chrome"
	BrowserFirefox = "firefox"
)

// Load reads cookies whose domain matches domainSuffix from the named browser
// and returns a Cookie header value of the form "name1=value1; name2=value2".
//
// browser may be one of "safari", "chrome", "firefox", or "auto" (try all
// registered browsers). An empty browser string is treated as "auto".
// Returns an error if no cookies are found or the store cannot be read.
func Load(browser, domainSuffix string) (string, error) {
	if browser == "" {
		browser = BrowserAuto
	}
	browser = strings.ToLower(strings.TrimSpace(browser))
	suffix := strings.ToLower(strings.TrimPrefix(domainSuffix, "."))

	// Yandex Browser is not discoverable via kooky; read its Chromium cookie
	// store directly.
	if browser == BrowserYandex {
		return loadYandex(domainSuffix)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Traverse all valid cookies (no domain filter) so we can tell apart two
	// failure modes: a store we could not read at all (zero cookies seen) vs a
	// store we read that simply has no cookie for the target domain.
	type entry struct {
		value   string
		expires time.Time
	}
	collected := make(map[string]entry)

	var totalSeen, browserSeen int

	seq := kooky.TraverseCookies(ctx, kooky.Valid).OnlyCookies()
	for cookie := range seq {
		totalSeen++
		if browser != BrowserAuto && !browserMatches(browser, cookie) {
			continue
		}
		browserSeen++

		dom := strings.ToLower(strings.TrimPrefix(cookie.Domain, "."))
		if dom != suffix && !strings.HasSuffix(dom, "."+suffix) {
			continue
		}
		name := cookie.Name
		if name == "" {
			continue
		}
		prev, ok := collected[name]
		// Prefer the cookie with the later expiry (more recently issued).
		if !ok || cookie.Expires.After(prev.expires) {
			collected[name] = entry{value: cookie.Value, expires: cookie.Expires}
		}
	}

	if len(collected) == 0 {
		return "", notFoundError(browser, suffix, totalSeen, browserSeen)
	}

	// Build a deterministic Cookie header (sorted by name).
	names := make([]string, 0, len(collected))
	for n := range collected {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(n)
		b.WriteByte('=')
		b.WriteString(collected[n].value)
	}
	return b.String(), nil
}

// notFoundError builds a descriptive error explaining why no cookies were found,
// including a macOS Full Disk Access hint when no store could be read at all.
func notFoundError(browser, suffix string, totalSeen, browserSeen int) error {
	switch {
	case totalSeen == 0:
		return fmt.Errorf(
			"could not read any browser cookie store (0 cookies). On macOS the cookie " +
				"files are protected: grant your terminal Full Disk Access in System " +
				"Settings \u2192 Privacy & Security \u2192 Full Disk Access, then retry. " +
				"Alternatively pass --cookie/--user-agent manually")
	case browser != BrowserAuto && browserSeen == 0:
		return fmt.Errorf(
			"read cookies from other browsers but none from %q (its store may be "+
				"locked, encrypted, or in a non-default profile). Try --browser-cookies "+
				"without a value to search all browsers, or pass --cookie manually", browser)
	default:
		return fmt.Errorf(
			"no cookies for domain %q found in browser %q \u2014 make sure you are logged "+
				"in to kino.pub in that browser, or pass --cookie manually", suffix, browser)
	}
}

// browserMatches reports whether a cookie originates from the requested browser.
// kooky exposes the source browser via Cookie.Browser.Browser(); we match on
// its name case-insensitively and tolerate the info being absent by matching all.
func browserMatches(browser string, cookie *kooky.Cookie) bool {
	if cookie == nil {
		return false
	}
	src := cookie.Browser
	if src == nil {
		return true
	}
	name := strings.ToLower(src.Browser())
	return strings.Contains(name, browser)
}
