package gui

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8765": true,
		"127.0.0.1":      true,
		"localhost:8765": true,
		"localhost":      true,
		"[::1]:8765":     true,
		"":               true,
		"evil.com:8765":  false,
		"example.com":    false,
		"10.0.0.5:8765":  false,
		"0.0.0.0:8765":   false,
	}
	for host, want := range cases {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestIsKinoPubHost(t *testing.T) {
	cases := map[string]bool{
		"kino.pub":         true,
		"KINO.PUB":         true,
		"cdn.kino.pub":     true,
		"a.b.kino.pub":     true,
		"kino.pub.evil.io": false,
		"notkino.pub":      false,
		"evil.com":         false,
	}
	for host, want := range cases {
		if got := isKinoPubHost(host); got != want {
			t.Errorf("isKinoPubHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":     true,
		"1.1.1.1":     true,
		"127.0.0.1":   false,
		"10.0.0.1":    false,
		"192.168.1.1": false,
		"172.16.0.1":  false,
		"169.254.0.1": false, // link-local
		"0.0.0.0":     false, // unspecified
		"::1":         false,
		"fc00::1":     false, // unique-local (private)
		"fe80::1":     false, // link-local
	}
	for ipStr, want := range cases {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			t.Fatalf("bad test IP %q", ipStr)
		}
		if got := isPublicIP(ip); got != want {
			t.Errorf("isPublicIP(%q) = %v, want %v", ipStr, got, want)
		}
	}
}

func TestResolveAuth(t *testing.T) {
	// Redirect credstore storage to an empty temp dir so the stored-credential
	// fallback finds nothing and we exercise the deterministic branches.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("explicit cookie wins, default UA applied", func(t *testing.T) {
		cookie, ua, err := resolveAuth("cf_clearance=abc", "", "")
		if err != nil {
			t.Fatalf("resolveAuth: %v", err)
		}
		if cookie != "cf_clearance=abc" {
			t.Errorf("cookie = %q, want explicit value", cookie)
		}
		if ua != defaultUserAgent {
			t.Errorf("ua = %q, want default", ua)
		}
	})

	t.Run("explicit user-agent preserved", func(t *testing.T) {
		_, ua, err := resolveAuth("c", "", "MyUA/1.0")
		if err != nil {
			t.Fatalf("resolveAuth: %v", err)
		}
		if ua != "MyUA/1.0" {
			t.Errorf("ua = %q, want MyUA/1.0", ua)
		}
	})

	t.Run("no credentials → empty cookie, default UA", func(t *testing.T) {
		cookie, ua, err := resolveAuth("", "", "")
		if err != nil {
			t.Fatalf("resolveAuth: %v", err)
		}
		if cookie != "" {
			t.Errorf("cookie = %q, want empty", cookie)
		}
		if ua != defaultUserAgent {
			t.Errorf("ua = %q, want default", ua)
		}
	})
}

func TestBuildRunConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("maps container and verbosity", func(t *testing.T) {
		cfg, err := buildRunConfig(RunRequest{
			URL:       "https://kino.pub/item/view/1",
			Quality:   "1080p",
			Container: "mp4",
			Verbosity: "verbose",
		})
		if err != nil {
			t.Fatalf("buildRunConfig: %v", err)
		}
		if cfg.Container != domain.ContainerMP4 {
			t.Errorf("Container = %v, want MP4", cfg.Container)
		}
		if cfg.Verbosity != domain.VerbosityVerbose {
			t.Errorf("Verbosity = %v, want verbose", cfg.Verbosity)
		}
	})

	t.Run("unknown container defaults to mkv", func(t *testing.T) {
		cfg, err := buildRunConfig(RunRequest{URL: "https://kino.pub/item/view/1"})
		if err != nil {
			t.Fatalf("buildRunConfig: %v", err)
		}
		if cfg.Container != domain.ContainerMKV {
			t.Errorf("Container = %v, want MKV", cfg.Container)
		}
	})

	t.Run("invalid season selection propagates error", func(t *testing.T) {
		if _, err := buildRunConfig(RunRequest{
			URL:     "https://kino.pub/item/view/1",
			Seasons: "not-a-number",
		}); err == nil {
			t.Fatal("expected error for invalid seasons, got nil")
		}
	})
}

func TestGuardLocalOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := NewServer("test", nil)
	ts := httptest.NewServer(srv.Handler()) // binds 127.0.0.1, so Host is loopback
	defer ts.Close()

	t.Run("loopback request allowed", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/health")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("cross-origin request rejected", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/settings", nil)
		req.Header.Set("Origin", "http://evil.example.com")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403 for cross-origin", resp.StatusCode)
		}
	})

	t.Run("forged non-loopback Host rejected", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/settings", nil)
		req.Host = "evil.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403 for forged host", resp.StatusCode)
		}
	})
}

func TestOpenPathAllowed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	lib := t.TempDir()
	srv := NewServer("test", nil)
	saved, err := srv.settings.save(Settings{OutputPath: lib, Container: "mkv", Concurrency: 2})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if saved.OutputPath != lib {
		t.Fatalf("settings not saved")
	}

	inside := filepath.Join(lib, "Show", "ep.mkv")
	if !srv.openPathAllowed(inside) {
		t.Errorf("path inside library should be allowed: %q", inside)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if srv.openPathAllowed(outside) {
		t.Errorf("path outside library should be rejected: %q", outside)
	}
}
