package gui

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
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

func TestParseEpisodeKeys(t *testing.T) {
	got, err := parseEpisodeKeys([]string{"S1E1", "S0E12", " S2E3 "})
	if err != nil {
		t.Fatalf("parseEpisodeKeys: %v", err)
	}
	want := []domain.EpisodeKey{{Season: 1, Episode: 1}, {Season: 0, Episode: 12}, {Season: 2, Episode: 3}}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Season != want[i].Season || got[i].Episode != want[i].Episode {
			t.Errorf("key %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := parseEpisodeKeys([]string{"nonsense"}); err == nil {
		t.Error("expected error for unparseable key")
	}
	if got, err := parseEpisodeKeys(nil); err != nil || got != nil {
		t.Errorf("nil keys: got %v, %v", got, err)
	}
}

func TestBuildRunConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("explicit episode keys become SelectedEpisodes", func(t *testing.T) {
		cfg, err := buildRunConfig(RunRequest{
			URL:         "https://kino.pub/item/view/1",
			EpisodeKeys: []string{"S1E1", "S1E2"},
		})
		if err != nil {
			t.Fatalf("buildRunConfig: %v", err)
		}
		if len(cfg.SelectedEpisodes) != 2 {
			t.Fatalf("SelectedEpisodes = %v, want 2 entries", cfg.SelectedEpisodes)
		}
	})

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

func TestDeleteLibrarySeries(t *testing.T) {
	root := t.TempDir()
	roots := []string{root}

	// A valid kinopub series dir inside the library root.
	seriesDir := filepath.Join(root, "Some Show")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seriesDir, stateFileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seriesDir, "S1E1.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reject: directory without a state file.
	noState := filepath.Join(root, "Random")
	_ = os.MkdirAll(noState, 0o755)
	if err := deleteLibrarySeries(noState, roots); err == nil {
		t.Error("expected rejection of a dir without a state file")
	}

	// Reject: the library root itself.
	if err := deleteLibrarySeries(root, roots); err == nil {
		t.Error("expected rejection of the library root itself")
	}

	// Reject: a path outside the configured roots.
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, stateFileName), []byte("{}"), 0o644)
	if err := deleteLibrarySeries(outside, roots); err == nil {
		t.Error("expected rejection of a path outside the library roots")
	}

	// Accept: the valid series dir, and it's actually removed.
	if err := deleteLibrarySeries(seriesDir, roots); err != nil {
		t.Fatalf("deleteLibrarySeries(valid): %v", err)
	}
	if _, err := os.Stat(seriesDir); !os.IsNotExist(err) {
		t.Error("series dir should have been removed")
	}
}

func TestFindStateDirs(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "ShowA")
	b := filepath.Join(root, "nested", "ShowB")
	_ = os.MkdirAll(a, 0o755)
	_ = os.MkdirAll(b, 0o755)
	_ = os.WriteFile(filepath.Join(a, stateFileName), []byte("{}"), 0o644)
	_ = os.WriteFile(filepath.Join(b, stateFileName), []byte("{}"), 0o644)

	dirs := findStateDirs(root)
	if len(dirs) != 2 {
		t.Fatalf("findStateDirs = %v, want 2 dirs (one nested)", dirs)
	}
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

func TestIsMovieDownload(t *testing.T) {
	ep := func(season, episode int) LibraryEpisode {
		return LibraryEpisode{Season: season, Episode: episode}
	}
	cases := []struct {
		name string
		s    LibrarySeries
		want bool
	}{
		// Stored kino.pub type wins, regardless of structure.
		{"type movie", LibrarySeries{Type: "movie", Episodes: []LibraryEpisode{ep(1, 1)}}, true},
		{"type documovie", LibrarySeries{Type: "documovie", Episodes: []LibraryEpisode{ep(1, 1), ep(1, 2)}}, true},
		{"type 4k movie", LibrarySeries{Type: "4k", Episodes: []LibraryEpisode{ep(1, 1)}}, true},
		{"type serial", LibrarySeries{Type: "serial", Episodes: []LibraryEpisode{ep(1, 1)}}, false},
		{"type docuserial", LibrarySeries{Type: "docuserial", Episodes: []LibraryEpisode{ep(1, 1)}}, false},
		{"type tvshow", LibrarySeries{Type: "tvshow", Episodes: []LibraryEpisode{ep(1, 1)}}, false},
		// No stored type → structural heuristic (legacy downloads).
		{"legacy single part", LibrarySeries{Episodes: []LibraryEpisode{ep(1, 1)}}, true},
		{"legacy multi episode", LibrarySeries{Episodes: []LibraryEpisode{ep(1, 1), ep(1, 2)}}, false},
		{"legacy multi season", LibrarySeries{Episodes: []LibraryEpisode{ep(1, 1), ep(2, 1)}}, false},
	}
	for _, c := range cases {
		if got := isMovieDownload(c.s); got != c.want {
			t.Errorf("isMovieDownload(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
