package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestWithAuth_InjectsHeaders(t *testing.T) {
	var gotCookie, gotUA, gotExtra string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotUA = r.Header.Get("User-Agent")
		gotExtra = r.Header.Get("X-Test")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	auth := domain.RequestAuth{
		Cookie:    "cf_clearance=abc; PHPSESSID=xyz",
		UserAgent: "Mozilla/5.0 (TestAgent)",
		Headers:   map[string]string{"X-Test": "1"},
	}
	client := WithAuth(srv.Client(), auth)

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if gotCookie != auth.Cookie {
		t.Errorf("Cookie = %q, want %q", gotCookie, auth.Cookie)
	}
	if gotUA != auth.UserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, auth.UserAgent)
	}
	if gotExtra != "1" {
		t.Errorf("X-Test = %q, want %q", gotExtra, "1")
	}
}

func TestWithAuth_EmptyAuthReturnsSameClient(t *testing.T) {
	client := &http.Client{}
	got := WithAuth(client, domain.RequestAuth{})
	if got != client {
		t.Error("expected the same client to be returned for empty auth")
	}
}

func TestWithAuth_DoesNotOverrideExistingCookie(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := WithAuth(srv.Client(), domain.RequestAuth{Cookie: "from=auth"})

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Cookie", "from=request")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if gotCookie != "from=request" {
		t.Errorf("Cookie = %q, want request-supplied value to win", gotCookie)
	}
}

func TestWithAuth_PreservesBaseTransport(t *testing.T) {
	// A custom base transport should still be invoked through the wrapper.
	called := false
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	client := &http.Client{Transport: base}

	wrapped := WithAuth(client, domain.RequestAuth{UserAgent: "x"})
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	resp, err := wrapped.Transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	resp.Body.Close()
	if !called {
		t.Error("expected base transport to be invoked")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
