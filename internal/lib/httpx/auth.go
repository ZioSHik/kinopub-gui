// Package httpx provides HTTP client helpers, notably an auth-injecting
// transport that applies a Cookie header, a User-Agent, and arbitrary extra
// headers to every outbound request. This lets the tool reuse a logged-in
// browser session to pass Cloudflare and kino.pub authentication.
package httpx

import (
	"net/http"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// authTransport wraps a base RoundTripper and injects authentication headers
// (Cookie, User-Agent, and any extra headers) into every request that does not
// already set them.
type authTransport struct {
	base http.RoundTripper
	auth domain.RequestAuth
}

// RoundTrip implements http.RoundTripper. It clones the request before mutating
// headers so it never modifies the caller's request (per the RoundTripper
// contract).
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())

	if t.auth.Cookie != "" && r.Header.Get("Cookie") == "" {
		r.Header.Set("Cookie", t.auth.Cookie)
	}
	if t.auth.UserAgent != "" {
		// Always set the User-Agent: Cloudflare's cf_clearance cookie is bound
		// to the UA that solved the challenge, so it must match exactly.
		r.Header.Set("User-Agent", t.auth.UserAgent)
	}
	for k, v := range t.auth.Headers {
		if r.Header.Get(k) == "" {
			r.Header.Set(k, v)
		}
	}

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// WithAuth returns a copy of client whose transport injects the given auth into
// every request. If auth is empty, the original client is returned unchanged.
// The base client's existing transport (e.g., a proxy transport) is preserved
// and wrapped.
func WithAuth(client *http.Client, auth domain.RequestAuth) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	if auth.IsZero() {
		return client
	}

	wrapped := *client // shallow copy so we don't mutate the caller's client
	wrapped.Transport = &authTransport{
		base: client.Transport,
		auth: auth,
	}
	return &wrapped
}
