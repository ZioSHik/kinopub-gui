package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// TestWithAuth_NilClient verifies WithAuth tolerates a nil client and still
// returns a usable, non-nil client with the auth transport installed.
func TestWithAuth_NilClient(t *testing.T) {
	got := WithAuth(nil, domain.RequestAuth{UserAgent: "x"})
	if got == nil {
		t.Fatal("expected non-nil client")
	}
	at, ok := got.Transport.(*authTransport)
	if !ok {
		t.Fatalf("expected *authTransport, got %T", got.Transport)
	}
	if at.auth.UserAgent != "x" {
		t.Errorf("UserAgent = %q, want %q", at.auth.UserAgent, "x")
	}
}

// TestWithAuth_NilClientEmptyAuth: a nil client with empty auth must still
// yield a non-nil client (the IsZero early-return path).
func TestWithAuth_NilClientEmptyAuth(t *testing.T) {
	got := WithAuth(nil, domain.RequestAuth{})
	if got == nil {
		t.Fatal("expected non-nil client even for empty auth")
	}
	if got.Transport != nil {
		t.Errorf("expected nil transport (default), got %T", got.Transport)
	}
}

// TestWithAuth_DoesNotMutateCallerClient ensures the caller's client is copied,
// not mutated: the original transport pointer must be unchanged.
func TestWithAuth_DoesNotMutateCallerClient(t *testing.T) {
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	orig := &http.Client{Transport: base}
	wrapped := WithAuth(orig, domain.RequestAuth{UserAgent: "x"})

	if wrapped == orig {
		t.Fatal("expected a copied client, got the same pointer")
	}
	// Original transport must still be the raw base, not the authTransport.
	if _, isAuth := orig.Transport.(*authTransport); isAuth {
		t.Error("caller's client transport was mutated")
	}
}

// TestWithAuth_PreservesClientFields verifies the shallow copy keeps fields like
// Timeout and CheckRedirect intact.
func TestWithAuth_PreservesClientFields(t *testing.T) {
	orig := &http.Client{}
	orig.Timeout = 1234
	wrapped := WithAuth(orig, domain.RequestAuth{Cookie: "a=b"})
	if wrapped.Timeout != orig.Timeout {
		t.Errorf("Timeout = %v, want %v", wrapped.Timeout, orig.Timeout)
	}
}

// TestAuthTransport_NilBaseUsesDefault: when base is nil, RoundTrip must fall
// back to http.DefaultTransport. We exercise this against a real httptest server.
func TestAuthTransport_NilBaseUsesDefault(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := &authTransport{base: nil, auth: domain.RequestAuth{UserAgent: "fallback-ua"}}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()
	if gotUA != "fallback-ua" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "fallback-ua")
	}
}

// TestAuthTransport_DoesNotMutateCallerRequest verifies the RoundTripper
// contract: the request passed in must not be modified (it is cloned).
func TestAuthTransport_DoesNotMutateCallerRequest(t *testing.T) {
	var seen http.Header
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Clone()
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	tr := &authTransport{base: base, auth: domain.RequestAuth{
		Cookie:    "c=1",
		UserAgent: "ua",
		Headers:   map[string]string{"X-Extra": "v"},
	}}

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// Original request must remain untouched.
	if req.Header.Get("Cookie") != "" {
		t.Errorf("original request Cookie mutated: %q", req.Header.Get("Cookie"))
	}
	if req.Header.Get("User-Agent") != "" {
		t.Errorf("original request User-Agent mutated: %q", req.Header.Get("User-Agent"))
	}
	if req.Header.Get("X-Extra") != "" {
		t.Errorf("original request X-Extra mutated: %q", req.Header.Get("X-Extra"))
	}
	// The cloned request the base saw must have all headers.
	if seen.Get("Cookie") != "c=1" || seen.Get("User-Agent") != "ua" || seen.Get("X-Extra") != "v" {
		t.Errorf("clone headers = %v, want all set", seen)
	}
}

// TestAuthTransport_UserAgentAlwaysOverrides: per the doc comment, the UA must
// always be forced (cf_clearance is bound to it), even if the request already
// has a different UA.
func TestAuthTransport_UserAgentAlwaysOverrides(t *testing.T) {
	var gotUA string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotUA = r.Header.Get("User-Agent")
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	tr := &authTransport{base: base, auth: domain.RequestAuth{UserAgent: "forced-ua"}}

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("User-Agent", "caller-ua")
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if gotUA != "forced-ua" {
		t.Errorf("User-Agent = %q, want forced auth UA to win", gotUA)
	}
}

// TestAuthTransport_ExtraHeaderDoesNotOverride: extra headers should only be set
// when absent, mirroring the Cookie behaviour.
func TestAuthTransport_ExtraHeaderDoesNotOverride(t *testing.T) {
	var got string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Get("X-Extra")
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	tr := &authTransport{base: base, auth: domain.RequestAuth{
		Headers: map[string]string{"X-Extra": "from-auth"},
	}}

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("X-Extra", "from-request")
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got != "from-request" {
		t.Errorf("X-Extra = %q, want request value to win", got)
	}
}

// TestAuthTransport_EmptyCookieNotSet: when auth.Cookie is empty, no Cookie
// header should be injected.
func TestAuthTransport_EmptyCookieNotSet(t *testing.T) {
	var hasCookie bool
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_, hasCookie = r.Header["Cookie"]
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	tr := &authTransport{base: base, auth: domain.RequestAuth{UserAgent: "ua"}}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if hasCookie {
		t.Error("Cookie header should not be set when auth.Cookie is empty")
	}
}

// TestAuthTransport_PropagatesContext ensures the request context survives the
// clone (Clone(req.Context())).
func TestAuthTransport_PropagatesContext(t *testing.T) {
	type ctxKey string
	const k ctxKey = "key"
	var gotVal any
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotVal = r.Context().Value(k)
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	tr := &authTransport{base: base, auth: domain.RequestAuth{UserAgent: "ua"}}

	ctx := context.WithValue(context.Background(), k, "v")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if gotVal != "v" {
		t.Errorf("context value = %v, want %q", gotVal, "v")
	}
}

// TestAuthTransport_MultipleExtraHeaders covers the map iteration path with more
// than one extra header.
func TestAuthTransport_MultipleExtraHeaders(t *testing.T) {
	var seen http.Header
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Clone()
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	tr := &authTransport{base: base, auth: domain.RequestAuth{
		Headers: map[string]string{"X-A": "1", "X-B": "2", "X-C": "3"},
	}}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	for k, want := range map[string]string{"X-A": "1", "X-B": "2", "X-C": "3"} {
		if got := seen.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
