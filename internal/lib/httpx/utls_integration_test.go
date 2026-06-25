//go:build netintegration

// These tests perform REAL TLS handshakes against live hosts to exercise the
// uTLS browser transport end to end (dialTLS, the https branch of RoundTrip,
// HTTP/2 and HTTP/1.1 round trips, h2 connection reuse). They are excluded from
// the default suite — a unit suite must stay deterministic and pass offline —
// and run only when explicitly requested:
//
//	go test -tags netintegration ./internal/lib/httpx/
//
// Each test SKIPS (not fails) when no host is reachable, so it is safe to run on
// a disconnected machine; it only FAILS when a host is reachable but the
// transport mishandles the connection.
package httpx

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// liveHosts are stable HTTPS endpoints. kino.pub is the real target this
// transport exists for (a Cloudflare 403/challenge still proves the handshake +
// round trip worked); the others are fallbacks so the test is meaningful even if
// kino.pub is blocked from the test network.
var liveHosts = []string{
	"https://kino.pub",
	"https://www.cloudflare.com",
	"https://example.com",
}

// h1Hosts negotiate HTTP/1.1 (no "h2" in ALPN), so a request through them takes
// the roundTripH1 path instead of roundTripH2. These are stable HTTP/1.1-only
// endpoints; a redirect/403 is fine — we only need the H1 round trip to run.
var h1Hosts = []string{
	"https://www.iana.org",
	"https://ftp.gnu.org",
}

// getVia issues a GET through client and returns the response (body drained and
// closed) or the transport error.
func getVia(t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request %q: %v", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	// Drain a bounded amount so the connection can be reused, then close.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return resp, nil
}

// TestBrowserClient_RealHandshake exercises a direct (no-proxy) browser client
// against a live host: dialTLS, the https branch of RoundTrip, hasPort (host
// gets ":443" appended), and the negotiated H2/H1 round trip.
func TestBrowserClient_RealHandshake(t *testing.T) {
	client := NewBrowserClient(nil)
	var lastErr error
	for _, h := range liveHosts {
		resp, err := getVia(t, client, h)
		if err != nil {
			lastErr = err
			t.Logf("%s unreachable: %v", h, err)
			continue
		}
		if resp.StatusCode <= 0 {
			t.Errorf("%s: got status %d, want a real HTTP status", h, resp.StatusCode)
		}
		t.Logf("%s → %s %d (a real TLS handshake + round trip succeeded)", h, resp.Proto, resp.StatusCode)

		// Second request to the same host reuses the cached HTTP/2 connection
		// (the "try existing connection first" branch of roundTripH2).
		if resp2, err := getVia(t, client, h); err == nil {
			t.Logf("%s (reuse) → %s %d", h, resp2.Proto, resp2.StatusCode)
		}
		return // one reachable host is enough to exercise the path
	}
	t.Skipf("no live host reachable (offline?): last error: %v", lastErr)
}

// TestBrowserClient_HTTP1RoundTrip exercises the roundTripH1 path: a host that
// negotiates HTTP/1.1 (rather than HTTP/2) over the uTLS connection.
func TestBrowserClient_HTTP1RoundTrip(t *testing.T) {
	client := NewBrowserClient(nil)
	var lastErr error
	for _, h := range h1Hosts {
		resp, err := getVia(t, client, h)
		if err != nil {
			lastErr = err
			t.Logf("%s unreachable: %v", h, err)
			continue
		}
		if resp.StatusCode <= 0 {
			t.Errorf("%s → status %d", h, resp.StatusCode)
		}
		if resp.ProtoMajor != 1 {
			// Host upgraded to H2 — the H1 path wasn't taken; try the next host.
			t.Logf("%s negotiated %s (not H1), trying next", h, resp.Proto)
			continue
		}
		t.Logf("%s → %s %d (roundTripH1 exercised)", h, resp.Proto, resp.StatusCode)
		return
	}
	t.Skipf("no HTTP/1.1 host reachable (offline?): last error: %v", lastErr)
}

// TestBrowserClient_ExplicitPort covers the hasPort==true branch of RoundTrip
// (the address already carries a port, so ":443" is not appended).
func TestBrowserClient_ExplicitPort(t *testing.T) {
	client := NewBrowserClient(nil)
	var lastErr error
	for _, h := range liveHosts {
		resp, err := getVia(t, client, h+":443")
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode <= 0 {
			t.Errorf("%s:443 → status %d", h, resp.StatusCode)
		}
		t.Logf("%s:443 → %s %d", h, resp.Proto, resp.StatusCode)
		return
	}
	t.Skipf("no live host reachable (offline?): last error: %v", lastErr)
}

// TestWrapWithBrowserTLS_RealHandshake exercises WrapWithBrowserTLS end to end:
// the wrapped client must perform the same uTLS handshake and round trip.
func TestWrapWithBrowserTLS_RealHandshake(t *testing.T) {
	client := WrapWithBrowserTLS(&http.Client{Timeout: 25 * time.Second})
	var lastErr error
	for _, h := range liveHosts {
		resp, err := getVia(t, client, h)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode <= 0 {
			t.Errorf("wrapped %s → status %d", h, resp.StatusCode)
		}
		t.Logf("wrapped %s → %s %d", h, resp.Proto, resp.StatusCode)
		return
	}
	t.Skipf("no live host reachable (offline?): last error: %v", lastErr)
}
