package gui

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/services/proxyprovider"
)

// randomKey returns n cryptographically-random bytes for signing proxy URLs.
// crypto/rand never fails on modern operating systems; if it does, the process
// panics rather than installing a predictable HMAC key that would make the
// /api/hls proxy forgeable.
func randomKey(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("kinopub: crypto/rand unavailable: " + err.Error())
	}
	return b
}

// HLS playback proxy for the in-app player.
//
// The browser cannot fetch kino.pub's CDN directly: the CDN may omit CORS
// headers, and segment hosts often differ from the manifest host. So manifests
// and segments are proxied through this localhost server (same-origin for the
// browser), and every child URL inside a manifest is rewritten to point back
// here. To avoid becoming an open proxy / SSRF vector, each proxied URL carries
// an HMAC signature keyed by a per-process secret: only URLs this server itself
// produced (by serving a /stream manifest and rewriting its children) are ever
// fetched.

var hlsAttrURIRe = regexp.MustCompile(`URI="([^"]+)"`)

// hlsUserAgent matches the official kino.pub Kodi addon's User-Agent, so the CDN
// treats the player's segment fetches like the sanctioned client.
const hlsUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"

// signHLS returns the HMAC tag for a target URL.
func (s *Server) signHLS(raw string) string {
	mac := hmac.New(sha256.New, s.hlsKey)
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

// proxiedHLSURL returns a same-origin, signed URL that proxies the given
// absolute media URL through handleHLSProxy.
func (s *Server) proxiedHLSURL(raw string) string {
	return "/api/hls?u=" + url.QueryEscape(raw) + "&s=" + s.signHLS(raw)
}

func (s *Server) handleHLSProxy(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	sig := r.URL.Query().Get("s")
	if raw == "" || !hmac.Equal([]byte(s.signHLS(raw)), []byte(sig)) {
		writeErr(w, http.StatusForbidden, "invalid stream token")
		return
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		writeErr(w, http.StatusBadRequest, "bad stream url")
		return
	}
	// Application-layer SSRF guard: reject signed URLs whose host is a
	// non-public IP literal. This catches IP literals injected by a
	// compromised manifest before we spend a concurrency slot or a dial.
	if !hlsTargetAllowed(raw) {
		writeErr(w, http.StatusBadRequest, "forbidden stream target")
		return
	}

	resp, err := s.fetchHLS(r.Context(), raw, r.Header.Get("Range"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	isManifest := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "mpegurl") ||
		strings.HasSuffix(strings.ToLower(target.Path), ".m3u8")

	if isManifest {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		rewritten := s.rewriteManifest(string(body), target)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.WriteString(w, rewritten)
		return
	}

	// Media segment / key: stream bytes through, preserving the headers the
	// player needs for ranged playback.
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// hlsTargetAllowed reports whether rawURL is safe to fetch as an HLS resource.
// It returns false when the URL's host is an IP literal that is not a public
// address (loopback, private, link-local, etc.), blocking SSRF via injected
// IP-literal URLs. Hostnames are allowed here; direct-mode dialing enforces
// the same rule at the TCP layer (defeating DNS rebinding too), and in proxy
// mode the proxy is user-configured and trusted.
func hlsTargetAllowed(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !isPublicIP(ip) {
		return false
	}
	return true
}

// ssrfSafeHLSClient returns an HTTP client for fetching HLS resources that is
// hardened against SSRF while preserving proxy/VPN playback:
//
//   - Direct mode: the transport's dialer is replaced with ssrfSafeDial, which
//     resolves the target hostname and refuses to connect to any non-public IP.
//     This also eliminates the DNS-rebinding TOCTOU window.
//   - Proxy mode: the user-configured proxy dialer is kept unchanged (the local
//     dialer cannot see the target IP through a SOCKS/HTTP tunnel, and the
//     proxy is user-supplied/trusted). The application-layer hlsTargetAllowed
//     check still blocks IP-literal targets before the proxy is contacted.
//   - Both modes: CheckRedirect caps hops at 5 and rejects any redirect whose
//     Location host is a non-public IP literal.
func (s *Server) ssrfSafeHLSClient() (*http.Client, error) {
	prov, err := proxyprovider.New(s.settings.get().Proxy)
	if err != nil {
		return nil, err
	}
	base := prov.HTTPClient()
	// Shallow-copy the client so we don't mutate the shared instance.
	hc := *base

	if prov.ProxyURL() == nil {
		// Direct mode: override the dialer to refuse non-public IP addresses.
		if tr, ok := base.Transport.(*http.Transport); ok {
			t := tr.Clone()
			t.DialContext = ssrfSafeDial
			hc.Transport = t
		}
	}
	// In both modes, cap redirects and reject redirects to non-public IPs.
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if ip := net.ParseIP(req.URL.Hostname()); ip != nil && !isPublicIP(ip) {
			return fmt.Errorf("refusing redirect to non-public address %s", req.URL.Host)
		}
		return nil
	}
	return &hc, nil
}

// fetchHLS fetches an upstream HLS resource from kino.pub's CDN, bounded by the
// concurrency semaphore and retrying transient failures. hls.js loads every
// rendition playlist at once; the CDN rate-limits that burst with 403s/429s that
// succeed on a prompt retry (the exact URL that 403s plays fine moments later).
func (s *Server) fetchHLS(ctx context.Context, raw, rangeHeader string) (*http.Response, error) {
	hc, err := s.ssrfSafeHLSClient()
	if err != nil {
		return nil, err
	}

	const attempts = 4
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// Back off between retries WITHOUT holding a concurrency slot, so a
			// retrying request never starves others (the bug that wedged
			// high-track-count titles like multi-dub serials).
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		// One concurrency slot per attempt, held only for this request.
		select {
		case s.hlsSem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if rerr != nil {
			<-s.hlsSem
			return nil, rerr
		}
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader) // forward Range so the player can seek
		}
		req.Header.Set("User-Agent", hlsUserAgent)
		req.Header.Set("Referer", "https://kino.pub/")
		resp, derr := hc.Do(req)
		if derr != nil {
			<-s.hlsSem
			lastErr = derr
			continue
		}
		if isTransientHLS(resp.StatusCode) && attempt < attempts-1 {
			resp.Body.Close()
			<-s.hlsSem
			continue
		}
		// Hold the slot until the caller finishes streaming the body.
		resp.Body = &slotBody{ReadCloser: resp.Body, sem: s.hlsSem}
		return resp, nil
	}
	return nil, lastErr
}

// slotBody releases a concurrency slot when the proxied response body is closed.
type slotBody struct {
	io.ReadCloser
	sem  chan struct{}
	once sync.Once
}

func (b *slotBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { <-b.sem })
	return err
}

// isTransientHLS reports whether a CDN status is worth retrying (rate-limit /
// load-balancer hiccups that clear on a prompt re-request).
func isTransientHLS(code int) bool {
	switch code {
	case http.StatusForbidden, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// rewriteManifest resolves every child URL in an HLS manifest against base and
// rewrites it to a signed, same-origin proxy URL, so the player fetches variant
// playlists, segments, audio renditions and keys through this server too.
func (s *Server) rewriteManifest(body string, base *url.URL) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			// Tags that embed a URI="..." (EXT-X-MEDIA audio renditions,
			// EXT-X-KEY, EXT-X-MAP, EXT-X-I-FRAME-STREAM-INF).
			if strings.Contains(trimmed, `URI="`) {
				lines[i] = hlsAttrURIRe.ReplaceAllStringFunc(line, func(m string) string {
					sub := hlsAttrURIRe.FindStringSubmatch(m)
					if len(sub) != 2 {
						return m
					}
					resolved := resolveRef(base, sub[1])
					// Defense in depth: do not mint a signature for a
					// non-public IP-literal target injected by the manifest.
					if !hlsTargetAllowed(resolved) {
						return m
					}
					return `URI="` + s.proxiedHLSURL(resolved) + `"`
				})
			}
			continue
		}
		// A bare URI line: a variant playlist or a media segment.
		resolved := resolveRef(base, trimmed)
		// Defense in depth: do not mint a signature for a non-public
		// IP-literal target injected by the manifest.
		if !hlsTargetAllowed(resolved) {
			continue
		}
		lines[i] = strings.Replace(line, trimmed, s.proxiedHLSURL(resolved), 1)
	}
	return strings.Join(lines, "\n")
}

func resolveRef(base *url.URL, ref string) string {
	u, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}
