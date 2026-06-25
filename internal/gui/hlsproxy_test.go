package gui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func escapeQ(s string) string { return url.QueryEscape(s) }

func newHLSServer() *Server {
	return &Server{hlsKey: []byte("test-key-32-bytes-long-padding!!"), hlsSem: make(chan struct{}, 4)}
}

func TestSignHLSDeterministicAndUnique(t *testing.T) {
	s := newHLSServer()
	a := s.signHLS("https://cdn.kino.pub/a.ts")
	again := s.signHLS("https://cdn.kino.pub/a.ts")
	b := s.signHLS("https://cdn.kino.pub/b.ts")
	if a == "" || a != again {
		t.Errorf("signature not deterministic: %q vs %q", a, again)
	}
	if a == b {
		t.Error("different URLs must yield different signatures")
	}
	// A different key produces a different signature for the same URL.
	s2 := &Server{hlsKey: []byte("another-32-byte-key-padding!!!!!")}
	if s2.signHLS("https://cdn.kino.pub/a.ts") == a {
		t.Error("different keys must yield different signatures")
	}
}

func TestProxiedHLSURL(t *testing.T) {
	s := newHLSServer()
	raw := "https://cdn.kino.pub/path?x=1&y=2"
	got := s.proxiedHLSURL(raw)
	if !strings.HasPrefix(got, "/api/hls?u=") {
		t.Errorf("missing prefix: %q", got)
	}
	if !strings.Contains(got, "&s="+s.signHLS(raw)) {
		t.Errorf("missing signature: %q", got)
	}
	// The original raw URL must be query-escaped (its & is encoded).
	if strings.Contains(got, "x=1&y=2&s=") {
		t.Errorf("raw query not escaped: %q", got)
	}
}

func TestHLSTargetAllowed(t *testing.T) {
	cases := map[string]bool{
		"https://cdn.kino.pub/a.ts": true, // hostname
		"https://8.8.8.8/a.ts":      true, // public IP literal
		"https://127.0.0.1/a.ts":    false,
		"https://10.0.0.1/a.ts":     false,
		"https://192.168.1.1/a.ts":  false,
		"https://[::1]/a.ts":        false,
		"https://169.254.0.1/a.ts":  false,
		"://bad":                    false, // unparseable URL → rejected
		"https://example.com/a.ts":  true,  // arbitrary hostname (no IP literal) → allowed
	}
	for raw, want := range cases {
		if got := hlsTargetAllowed(raw); got != want {
			t.Errorf("hlsTargetAllowed(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestIsTransientHLS(t *testing.T) {
	transient := []int{403, 429, 500, 502, 503, 504}
	for _, c := range transient {
		if !isTransientHLS(c) {
			t.Errorf("status %d should be transient", c)
		}
	}
	for _, c := range []int{200, 206, 301, 404, 401} {
		if isTransientHLS(c) {
			t.Errorf("status %d should NOT be transient", c)
		}
	}
}

func TestResolveRef(t *testing.T) {
	base := mustParseURL(t, "https://cdn.kino.pub/series/master.m3u8")
	cases := map[string]string{
		"audio/aud.m3u8":          "https://cdn.kino.pub/series/audio/aud.m3u8",
		"/abs/seg.ts":             "https://cdn.kino.pub/abs/seg.ts",
		"https://other.host/x.ts": "https://other.host/x.ts",
		"../seg0.ts":              "https://cdn.kino.pub/seg0.ts",
	}
	for ref, want := range cases {
		if got := resolveRef(base, ref); got != want {
			t.Errorf("resolveRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestRewriteManifest(t *testing.T) {
	s := newHLSServer()
	base := mustParseURL(t, "https://cdn.kino.pub/series/master.m3u8")
	manifest := strings.Join([]string{
		"#EXTM3U",
		`#EXT-X-MEDIA:TYPE=AUDIO,URI="audio/rus.m3u8",NAME="rus"`,
		"#EXT-X-STREAM-INF:BANDWIDTH=1000",
		"video/1080.m3u8",
		"", // blank line preserved
		"#EXT-X-KEY:METHOD=AES-128,URI=\"https://10.0.0.1/key.bin\"", // non-public → NOT rewritten
	}, "\n")

	out := s.rewriteManifest(manifest, base)
	lines := strings.Split(out, "\n")

	// The audio URI is rewritten to a signed proxy URL.
	if !strings.Contains(lines[1], `URI="/api/hls?u=`) {
		t.Errorf("audio rendition not rewritten: %q", lines[1])
	}
	// The bare variant playlist line is rewritten.
	if !strings.HasPrefix(lines[3], "/api/hls?u=") {
		t.Errorf("variant playlist not rewritten: %q", lines[3])
	}
	// Header tag without URI is left untouched.
	if lines[0] != "#EXTM3U" || lines[2] != "#EXT-X-STREAM-INF:BANDWIDTH=1000" {
		t.Errorf("non-URI tags altered: %q / %q", lines[0], lines[2])
	}
	// The non-public IP-literal key URI is NOT signed (defense in depth).
	if strings.Contains(lines[5], "/api/hls?u=") {
		t.Errorf("non-public key URI should not be rewritten: %q", lines[5])
	}
	if !strings.Contains(lines[5], "https://10.0.0.1/key.bin") {
		t.Errorf("original non-public URI should be preserved: %q", lines[5])
	}
}

func TestHandleHLSProxy_RejectsBadSignature(t *testing.T) {
	s := newHLSServer()
	req := httptest.NewRequest("GET", "/api/hls?u="+"https%3A%2F%2Fcdn.kino.pub%2Fa.ts"+"&s=wrong", nil)
	w := httptest.NewRecorder()
	s.handleHLSProxy(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("bad signature: status = %d, want 403", w.Code)
	}
}

func TestHandleHLSProxy_RejectsMissingURL(t *testing.T) {
	s := newHLSServer()
	req := httptest.NewRequest("GET", "/api/hls", nil)
	w := httptest.NewRecorder()
	s.handleHLSProxy(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("missing url: status = %d, want 403", w.Code)
	}
}

func TestHandleHLSProxy_RejectsBadScheme(t *testing.T) {
	s := newHLSServer()
	raw := "ftp://cdn.kino.pub/a.ts"
	req := httptest.NewRequest("GET", "/api/hls?u="+escapeQ(raw)+"&s="+s.signHLS(raw), nil)
	w := httptest.NewRecorder()
	s.handleHLSProxy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad scheme: status = %d, want 400", w.Code)
	}
}

func TestHandleHLSProxy_RejectsNonPublicTarget(t *testing.T) {
	s := newHLSServer()
	raw := "https://127.0.0.1/a.ts"
	req := httptest.NewRequest("GET", "/api/hls?u="+escapeQ(raw)+"&s="+s.signHLS(raw), nil)
	w := httptest.NewRecorder()
	s.handleHLSProxy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("non-public target: status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "forbidden stream target") {
		t.Errorf("unexpected body: %q", w.Body.String())
	}
}

func TestSlotBodyReleasesOnce(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // occupy the slot
	body := &slotBody{ReadCloser: io.NopCloser(strings.NewReader("x")), sem: sem}
	if err := body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Slot should be released exactly once; a second close must not double-release
	// (which would panic on an empty channel receive in a different design, but
	// here once.Do guards it — verify the slot is now free).
	select {
	case sem <- struct{}{}:
	default:
		t.Fatal("slot was not released on Close")
	}
	_ = body.Close() // second close is a no-op for the slot
}
