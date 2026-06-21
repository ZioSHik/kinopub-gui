package proxyprovider

import (
	"errors"
	"net/http"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

// fakeEnv builds an EnvLookupFunc from a map.
func fakeEnv(m map[string]string) EnvLookupFunc {
	return func(key string) string {
		return m[key]
	}
}

// --- Proxy selection precedence ---

func TestNew_ExplicitProxy_HTTP(t *testing.T) {
	p, err := New("http://proxy.example.com:8080", WithEnvLookup(fakeEnv(map[string]string{
		"HTTP_PROXY": "http://system.example.com:3128",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mode() != domain.ProxyExplicit {
		t.Errorf("expected ProxyExplicit, got %v", p.Mode())
	}
}

func TestNew_ExplicitProxy_SOCKS5(t *testing.T) {
	p, err := New("socks5://proxy.example.com:1080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mode() != domain.ProxyExplicit {
		t.Errorf("expected ProxyExplicit, got %v", p.Mode())
	}
}

func TestNew_SystemProxy(t *testing.T) {
	p, err := New("", WithEnvLookup(fakeEnv(map[string]string{
		"HTTP_PROXY": "http://system.example.com:3128",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mode() != domain.ProxySystem {
		t.Errorf("expected ProxySystem, got %v", p.Mode())
	}
}

func TestNew_SystemProxy_AllProxy(t *testing.T) {
	p, err := New("", WithEnvLookup(fakeEnv(map[string]string{
		"ALL_PROXY": "socks5://tunnel.example.com:1080",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mode() != domain.ProxySystem {
		t.Errorf("expected ProxySystem, got %v", p.Mode())
	}
}

func TestNew_DirectWhenNoProxy(t *testing.T) {
	p, err := New("", WithEnvLookup(fakeEnv(map[string]string{})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mode() != domain.ProxyDirect {
		t.Errorf("expected ProxyDirect, got %v", p.Mode())
	}
}

// --- Validation ---

func TestNew_InvalidScheme(t *testing.T) {
	_, err := New("ftp://proxy.example.com:21")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
	if !errors.Is(err, domain.ErrInvalidProxyURL) {
		t.Errorf("expected ErrInvalidProxyURL, got %v", err)
	}
}

func TestNew_MissingHost(t *testing.T) {
	_, err := New("http://")
	if err == nil {
		t.Fatal("expected error for missing host")
	}
	if !errors.Is(err, domain.ErrInvalidProxyURL) {
		t.Errorf("expected ErrInvalidProxyURL, got %v", err)
	}
}

func TestNew_EmptyScheme(t *testing.T) {
	_, err := New("://proxy.example.com")
	if err == nil {
		t.Fatal("expected error for empty scheme")
	}
	if !errors.Is(err, domain.ErrInvalidProxyURL) {
		t.Errorf("expected ErrInvalidProxyURL, got %v", err)
	}
}

func TestNew_NoScheme(t *testing.T) {
	_, err := New("proxy.example.com:8080")
	if err == nil {
		t.Fatal("expected error for missing scheme")
	}
	if !errors.Is(err, domain.ErrInvalidProxyURL) {
		t.Errorf("expected ErrInvalidProxyURL, got %v", err)
	}
}

// --- HTTPClient ---

func TestHTTPClient_ReturnsNonNil(t *testing.T) {
	p, err := New("", WithEnvLookup(fakeEnv(map[string]string{})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	client := p.HTTPClient()
	if client == nil {
		t.Fatal("expected non-nil http.Client")
	}
}

func TestHTTPClient_WithProxy_HasTransport(t *testing.T) {
	p, err := New("http://proxy.example.com:8080", WithEnvLookup(fakeEnv(map[string]string{})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	client := p.HTTPClient()
	if client.Transport == nil {
		t.Fatal("expected non-nil Transport for proxied client")
	}
	_, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
}

// --- FFmpegEnv ---

func TestFFmpegEnv_Direct(t *testing.T) {
	p, err := New("", WithEnvLookup(fakeEnv(map[string]string{})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, err := p.FFmpegEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env != nil {
		t.Errorf("expected nil env for direct, got %v", env)
	}
}

func TestFFmpegEnv_HTTPProxy(t *testing.T) {
	p, err := New("http://proxy.example.com:8080", WithEnvLookup(fakeEnv(map[string]string{})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, err := p.FFmpegEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) < 2 {
		t.Fatalf("expected at least 2 env entries, got %d", len(env))
	}
	if env[0] != "http_proxy=http://proxy.example.com:8080" {
		t.Errorf("unexpected http_proxy: %s", env[0])
	}
	if env[1] != "https_proxy=http://proxy.example.com:8080" {
		t.Errorf("unexpected https_proxy: %s", env[1])
	}
}

func TestFFmpegEnv_HTTPSProxy(t *testing.T) {
	p, err := New("https://secure.proxy.com:443", WithEnvLookup(fakeEnv(map[string]string{})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, err := p.FFmpegEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) < 2 {
		t.Fatalf("expected at least 2 env entries, got %d", len(env))
	}
	if env[0] != "http_proxy=https://secure.proxy.com:443" {
		t.Errorf("unexpected http_proxy: %s", env[0])
	}
	if env[1] != "https_proxy=https://secure.proxy.com:443" {
		t.Errorf("unexpected https_proxy: %s", env[1])
	}
}

func TestFFmpegEnv_SOCKS5_ReturnsError(t *testing.T) {
	p, err := New("socks5://proxy.example.com:1080", WithEnvLookup(fakeEnv(map[string]string{})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = p.FFmpegEnv()
	if err == nil {
		t.Fatal("expected error for socks5 ffmpeg env")
	}
	if !errors.Is(err, domain.ErrProxyUnsupportedFFmpeg) {
		t.Errorf("expected ErrProxyUnsupportedFFmpeg, got %v", err)
	}
}

func TestFFmpegEnv_IncludesNoProxy(t *testing.T) {
	p, err := New("http://proxy.example.com:8080", WithEnvLookup(fakeEnv(map[string]string{
		"NO_PROXY": "localhost,127.0.0.1",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, err := p.FFmpegEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(env) != 3 {
		t.Fatalf("expected 3 env entries, got %d: %v", len(env), env)
	}
	if env[2] != "no_proxy=localhost,127.0.0.1" {
		t.Errorf("unexpected no_proxy: %s", env[2])
	}
}

// --- NO_PROXY exclusion ---

func TestIsExcluded_ExactMatch(t *testing.T) {
	p := &Provider{}
	hosts := []string{"localhost", "example.com"}

	if !p.isExcluded("localhost", hosts) {
		t.Error("expected localhost to be excluded")
	}
	if !p.isExcluded("example.com", hosts) {
		t.Error("expected example.com to be excluded")
	}
	if p.isExcluded("other.com", hosts) {
		t.Error("expected other.com to NOT be excluded")
	}
}

func TestIsExcluded_DotPrefix(t *testing.T) {
	p := &Provider{}
	hosts := []string{".example.com"}

	if !p.isExcluded("sub.example.com", hosts) {
		t.Error("expected sub.example.com to be excluded")
	}
	if p.isExcluded("example.com", hosts) {
		t.Error("expected example.com to NOT be excluded with dot prefix")
	}
}

func TestIsExcluded_SuffixMatch(t *testing.T) {
	p := &Provider{}
	hosts := []string{"example.com"}

	if !p.isExcluded("sub.example.com", hosts) {
		t.Error("expected sub.example.com to be excluded via suffix match")
	}
}

func TestIsExcluded_Wildcard(t *testing.T) {
	p := &Provider{}
	hosts := []string{"*"}

	if !p.isExcluded("anything.com", hosts) {
		t.Error("expected wildcard to exclude everything")
	}
}

func TestIsExcluded_CaseInsensitive(t *testing.T) {
	p := &Provider{}
	hosts := []string{"example.com"}

	if !p.isExcluded("EXAMPLE.COM", hosts) {
		t.Error("expected case-insensitive match")
	}
}

func TestIsExcluded_EmptyHostname(t *testing.T) {
	p := &Provider{}
	hosts := []string{"example.com"}

	if p.isExcluded("", hosts) {
		t.Error("expected empty hostname to NOT be excluded")
	}
}

// --- Interface compliance ---

var _ domain.ProxyProvider = (*Provider)(nil)
